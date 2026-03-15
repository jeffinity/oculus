package service

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/data"
	bookkeepingv1 "github.com/jeffinity/oculus/proto/bookkeeping/v1"
)

type BookkeepingService struct {
	bookkeepingv1.UnimplementedBookkeepingServiceServer

	assetRepo  *data.AssetRepo
	loanRepo   *data.LoanRepo
	ledgerRepo *data.LedgerRepo
	log        *log.Helper
}

type loanContext struct {
	loans             []data.Loan
	adjustmentsByLoan map[string][]data.LoanRateAdjustment
	prepaymentsByLoan map[string][]data.LoanPrepayment
	computations      map[string]*loanComputation
}

var supportedLedgerCategories = map[string]struct{}{
	"餐饮": {}, "购物": {}, "日用": {}, "交通": {}, "汽车": {}, "蔬菜": {}, "水果": {}, "零食": {},
	"运动": {}, "娱乐": {}, "通讯": {}, "服饰": {}, "美容": {}, "住房": {}, "居家": {}, "孩子": {},
	"长辈": {}, "社交": {}, "旅行": {}, "烟酒": {}, "数码": {}, "医疗": {}, "书籍": {}, "学习": {},
	"宠物": {}, "礼金": {}, "礼物": {}, "办公": {}, "维修": {}, "捐赠": {}, "彩票": {}, "亲友": {},
	"快递": {}, "设置": {},
}

func NewBookkeepingService(assetRepo *data.AssetRepo, loanRepo *data.LoanRepo, ledgerRepo *data.LedgerRepo, logger log.Logger) *BookkeepingService {
	return &BookkeepingService{
		assetRepo:  assetRepo,
		loanRepo:   loanRepo,
		ledgerRepo: ledgerRepo,
		log:        log.NewHelper(log.With(logger, "module", "bookkeeping/BookkeepingService")),
	}
}

//nolint:cyclop,funlen // service method orchestrates multiple repository calls and validations.
func (s *BookkeepingService) AssetList(ctx context.Context, req *bookkeepingv1.AssetListRequest) (*bookkeepingv1.AssetListReply, error) {
	if req == nil {
		req = &bookkeepingv1.AssetListRequest{}
	}
	includeLoan := req.GetIncludeLoan()

	items, err := s.assetRepo.List(ctx, req.GetStartYm(), req.GetEndYm())
	if err != nil {
		s.log.Errorf("query assets failed: %v", err)
		return nil, status.Error(codes.Internal, "query assets failed")
	}

	loans := make([]data.Loan, 0)
	adjustmentsByLoanID := map[string][]data.LoanRateAdjustment{}
	prepaymentsByLoanID := map[string][]data.LoanPrepayment{}
	if includeLoan {
		loans, err = s.loanRepo.List(ctx)
		if err != nil {
			s.log.Errorf("query loans failed: %v", err)
			return nil, status.Error(codes.Internal, "query loans failed")
		}
		loanIDs := make([]string, 0, len(loans))
		for i := range loans {
			loanIDs = append(loanIDs, loans[i].LoanID)
		}
		adjustments, qErr := s.loanRepo.ListRateAdjustments(ctx, loanIDs)
		if qErr != nil {
			s.log.Errorf("query loan adjustments failed: %v", qErr)
			return nil, status.Error(codes.Internal, "query loan adjustments failed")
		}
		adjustmentsByLoanID = make(map[string][]data.LoanRateAdjustment, len(loanIDs))
		for i := range adjustments {
			adjustmentsByLoanID[adjustments[i].LoanID] = append(adjustmentsByLoanID[adjustments[i].LoanID], adjustments[i])
		}
		for loanID := range adjustmentsByLoanID {
			sort.Slice(adjustmentsByLoanID[loanID], func(i, j int) bool {
				di := normalizeAdjustmentEffectiveDate(adjustmentsByLoanID[loanID][i])
				dj := normalizeAdjustmentEffectiveDate(adjustmentsByLoanID[loanID][j])
				if di.Equal(dj) {
					return adjustmentsByLoanID[loanID][i].CreatedAt.Before(adjustmentsByLoanID[loanID][j].CreatedAt)
				}
				return di.Before(dj)
			})
		}

		prepayments, qErr := s.loanRepo.ListPrepayments(ctx, loanIDs)
		if qErr != nil {
			s.log.Errorf("query loan prepayments failed: %v", qErr)
			return nil, status.Error(codes.Internal, "query loan prepayments failed")
		}
		prepaymentsByLoanID = make(map[string][]data.LoanPrepayment, len(loanIDs))
		for i := range prepayments {
			prepaymentsByLoanID[prepayments[i].LoanID] = append(prepaymentsByLoanID[prepayments[i].LoanID], prepayments[i])
		}
		for loanID := range prepaymentsByLoanID {
			sort.Slice(prepaymentsByLoanID[loanID], func(i, j int) bool {
				di, _ := parseDate(prepaymentsByLoanID[loanID][i].PrepaymentDate)
				dj, _ := parseDate(prepaymentsByLoanID[loanID][j].PrepaymentDate)
				if di.Equal(dj) {
					return prepaymentsByLoanID[loanID][i].CreatedAt.Before(prepaymentsByLoanID[loanID][j].CreatedAt)
				}
				return di.Before(dj)
			})
		}
	}

	reply := &bookkeepingv1.AssetListReply{
		Items: make([]*bookkeepingv1.AssetItem, 0, len(items)),
	}
	for i := range items {
		assetValue, err := parseMoney(items[i].Asset)
		if err != nil {
			s.log.Errorf("invalid asset value ym=%s value=%q err=%v", items[i].YM, items[i].Asset, err)
			return nil, status.Error(codes.Internal, "invalid asset value")
		}
		baseLiability, err := parseMoney(items[i].Liability)
		if err != nil {
			s.log.Errorf("invalid liability value ym=%s value=%q err=%v", items[i].YM, items[i].Liability, err)
			return nil, status.Error(codes.Internal, "invalid liability value")
		}
		baseNetAsset, err := parseMoney(items[i].NetAsset)
		if err != nil {
			baseNetAsset = assetValue - baseLiability
		}

		loanItems := make([]*bookkeepingv1.LoanBalanceItem, 0, len(loans))
		loanLiability := 0.0
		if includeLoan {
			for j := range loans {
				snapshot, calcErr := loanSnapshotForYM(loans[j], adjustmentsByLoanID[loans[j].LoanID], prepaymentsByLoanID[loans[j].LoanID], items[i].YM)
				if calcErr != nil {
					s.log.Errorf("loan schedule calc failed loan_id=%s ym=%s err=%v", loans[j].LoanID, items[i].YM, calcErr)
					return nil, status.Error(codes.Internal, "loan schedule calc failed")
				}
				if !snapshot.Include || snapshot.RemainingPrincipal <= 0 {
					continue
				}

				loanLiability += snapshot.RemainingPrincipal
				loanItems = append(loanItems, &bookkeepingv1.LoanBalanceItem{
					LoanId:              loans[j].LoanID,
					LoanName:            loans[j].LoanName,
					LoanType:            toProtoLoanType(loans[j].LoanType),
					RemainingPrincipal:  formatMoney(snapshot.RemainingPrincipal),
					MonthlyPrincipal:    formatMoney(snapshot.MonthlyPrincipal),
					MonthlyInterest:     formatMoney(snapshot.MonthlyInterest),
					LoanDate:            loans[j].LoanDate,
					PrepaymentPrincipal: formatMoney(snapshot.PrepaymentPrincipal),
				})
			}
			sort.Slice(loanItems, func(a, b int) bool {
				return loanItems[a].LoanId < loanItems[b].LoanId
			})
		}

		totalLiability := baseLiability
		netAsset := baseNetAsset
		if includeLoan {
			totalLiability = baseLiability + loanLiability
			netAsset = assetValue - totalLiability
		}

		reply.Items = append(reply.Items, &bookkeepingv1.AssetItem{
			Ym:        items[i].YM,
			Asset:     formatMoney(assetValue),
			NetAsset:  formatMoney(netAsset),
			Liability: formatMoney(totalLiability),
			Remark:    items[i].Remark,
			Loans:     loanItems,
		})
	}
	return reply, nil
}

func (s *BookkeepingService) UpsertLedgerEntry(ctx context.Context, req *bookkeepingv1.UpsertLedgerEntryRequest) (*bookkeepingv1.UpsertLedgerEntryReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	entryDate, err := parseDate(strings.TrimSpace(req.GetEntryDate()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "entry date must be YYYY-MM-DD")
	}
	amount, err := parseMoney(req.GetAmount())
	if err != nil || amount == 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be a non-zero number")
	}
	category := strings.TrimSpace(req.GetCategory())
	if category == "" {
		return nil, status.Error(codes.InvalidArgument, "category is required")
	}
	if _, ok := supportedLedgerCategories[category]; !ok {
		return nil, status.Error(codes.InvalidArgument, "category is unsupported")
	}
	remark := strings.TrimSpace(req.GetRemark())
	ym := strings.ReplaceAll(formatDate(entryDate)[:7], "-", ".")

	now := time.Now()
	item := &data.Ledger{
		EntryDate: formatDate(entryDate),
		YM:        ym,
		Amount:    formatMoney(amount),
		Category:  category,
		Remark:    remark,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err = s.ledgerRepo.Upsert(ctx, item); err != nil {
		s.log.Errorf("upsert ledger entry failed: date=%s category=%s amount=%s err=%v", item.EntryDate, item.Category, item.Amount, err)
		return nil, status.Error(codes.Internal, "upsert ledger entry failed")
	}
	return &bookkeepingv1.UpsertLedgerEntryReply{
		EntryDate: item.EntryDate,
		Ym:        item.YM,
		Amount:    item.Amount,
		Category:  item.Category,
		Remark:    item.Remark,
	}, nil
}

func (s *BookkeepingService) LedgerList(ctx context.Context, req *bookkeepingv1.LedgerListRequest) (*bookkeepingv1.LedgerListReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ym := strings.TrimSpace(req.GetYm())
	if _, err := parseYM(ym); err != nil {
		return nil, status.Error(codes.InvalidArgument, "ym must be YYYY.MM")
	}
	items, err := s.ledgerRepo.ListByYM(ctx, ym)
	if err != nil {
		s.log.Errorf("query ledgers failed: ym=%s err=%v", ym, err)
		return nil, status.Error(codes.Internal, "query ledgers failed")
	}
	reply := &bookkeepingv1.LedgerListReply{
		Items: make([]*bookkeepingv1.LedgerItem, 0, len(items)),
	}
	for i := range items {
		reply.Items = append(reply.Items, &bookkeepingv1.LedgerItem{
			EntryDate: items[i].EntryDate,
			Ym:        items[i].YM,
			Amount:    items[i].Amount,
			Category:  items[i].Category,
			Remark:    items[i].Remark,
		})
	}
	return reply, nil
}

//nolint:cyclop,funlen // service method branches by dimension and keeps errors explicit.
func (s *BookkeepingService) LedgerStats(ctx context.Context, req *bookkeepingv1.LedgerStatsRequest) (*bookkeepingv1.LedgerStatsReply, error) {
	if req == nil {
		req = &bookkeepingv1.LedgerStatsRequest{}
	}
	dimension := strings.ToLower(strings.TrimSpace(req.GetDimension()))
	if dimension == "" {
		dimension = "month"
	}
	if dimension != "month" && dimension != "year" {
		return nil, status.Error(codes.InvalidArgument, "dimension must be month or year")
	}

	availableYMs, err := s.ledgerRepo.ListAvailableYMs(ctx)
	if err != nil {
		s.log.Errorf("query available ledger yms failed: %v", err)
		return nil, status.Error(codes.Internal, "query ledger stats failed")
	}
	availableYears := extractYearsFromYMs(availableYMs)

	resolvedYM := strings.TrimSpace(req.GetYm())
	resolvedYear := strings.TrimSpace(req.GetYear())
	var trend []data.LedgerExpensePoint
	var rankings []data.LedgerExpenseRank

	if dimension == "month" {
		if _, err = parseYM(resolvedYM); err != nil {
			resolvedYM = pickLatestYM(availableYMs)
		}
		if resolvedYM == "" {
			now := time.Now().UTC()
			resolvedYM = fmt.Sprintf("%04d.%02d", now.Year(), int(now.Month()))
		}
		resolvedYear = resolvedYM[:4]

		trend, err = s.ledgerRepo.ExpenseTrendByYM(ctx, resolvedYM)
		if err != nil {
			s.log.Errorf("query ledger monthly trend failed: ym=%s err=%v", resolvedYM, err)
			return nil, status.Error(codes.Internal, "query ledger stats failed")
		}
		rankings, err = s.ledgerRepo.ExpenseRankingByYM(ctx, resolvedYM)
		if err != nil {
			s.log.Errorf("query ledger monthly rankings failed: ym=%s err=%v", resolvedYM, err)
			return nil, status.Error(codes.Internal, "query ledger stats failed")
		}
	} else {
		if !isValidYear(resolvedYear) {
			resolvedYear = pickLatestYear(availableYears)
		}
		if resolvedYear == "" {
			resolvedYear = fmt.Sprintf("%04d", time.Now().UTC().Year())
		}
		resolvedYM = pickLatestYMInYear(availableYMs, resolvedYear)

		trend, err = s.ledgerRepo.ExpenseTrendByYear(ctx, resolvedYear)
		if err != nil {
			s.log.Errorf("query ledger yearly trend failed: year=%s err=%v", resolvedYear, err)
			return nil, status.Error(codes.Internal, "query ledger stats failed")
		}
		rankings, err = s.ledgerRepo.ExpenseRankingByYear(ctx, resolvedYear)
		if err != nil {
			s.log.Errorf("query ledger yearly rankings failed: year=%s err=%v", resolvedYear, err)
			return nil, status.Error(codes.Internal, "query ledger stats failed")
		}
	}

	reply := &bookkeepingv1.LedgerStatsReply{
		Dimension:      dimension,
		Ym:             resolvedYM,
		Year:           resolvedYear,
		AvailableYms:   availableYMs,
		AvailableYears: availableYears,
		TrendPoints:    make([]*bookkeepingv1.LedgerTrendPoint, 0, len(trend)),
		ExpenseRankings: make([]*bookkeepingv1.LedgerExpenseRankItem,
			0, len(rankings)),
	}

	for i := range trend {
		reply.TrendPoints = append(reply.TrendPoints, &bookkeepingv1.LedgerTrendPoint{
			Key:     trend[i].Key,
			Expense: formatMoney(trend[i].Total),
		})
	}

	totalExpense := 0.0
	for i := range rankings {
		totalExpense += rankings[i].Total
		reply.ExpenseRankings = append(reply.ExpenseRankings, &bookkeepingv1.LedgerExpenseRankItem{
			Rank:     uint32(i + 1),
			Category: rankings[i].Category,
			Expense:  formatMoney(rankings[i].Total),
		})
	}
	reply.TotalExpense = formatMoney(totalExpense)

	return reply, nil
}

func extractYearsFromYMs(yms []string) []string {
	if len(yms) == 0 {
		return []string{}
	}
	years := make([]string, 0, len(yms))
	seen := make(map[string]struct{}, len(yms))
	for i := range yms {
		if len(yms[i]) < 4 {
			continue
		}
		year := yms[i][:4]
		if !isValidYear(year) {
			continue
		}
		if _, ok := seen[year]; ok {
			continue
		}
		seen[year] = struct{}{}
		years = append(years, year)
	}
	sort.Strings(years)
	return years
}

func pickLatestYM(yms []string) string {
	if len(yms) == 0 {
		return ""
	}
	return yms[len(yms)-1]
}

func pickLatestYear(years []string) string {
	if len(years) == 0 {
		return ""
	}
	return years[len(years)-1]
}

func pickLatestYMInYear(yms []string, year string) string {
	prefix := year + "."
	for i := len(yms) - 1; i >= 0; i-- {
		if strings.HasPrefix(yms[i], prefix) {
			return yms[i]
		}
	}
	return ""
}

func isValidYear(raw string) bool {
	if len(raw) != 4 {
		return false
	}
	_, err := strconv.Atoi(raw)
	return err == nil
}

func (s *BookkeepingService) CreateMortgageLoan(ctx context.Context, req *bookkeepingv1.CreateMortgageLoanRequest) (*bookkeepingv1.CreateMortgageLoanReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	principal, err := parseMoney(req.GetInitialPrincipal())
	if err != nil || principal <= 0 {
		return nil, status.Error(codes.InvalidArgument, "initial principal is invalid")
	}
	annualRate, err := parseRate(req.GetAnnualRate())
	if err != nil || annualRate < 0 {
		return nil, status.Error(codes.InvalidArgument, "annual rate is invalid")
	}
	termYears := req.GetTermYears()
	if termYears == 0 {
		return nil, status.Error(codes.InvalidArgument, "term years must be greater than 0")
	}
	if termYears > 100 {
		return nil, status.Error(codes.InvalidArgument, "term years is too large")
	}
	loanDate, err := parseDate(strings.TrimSpace(req.GetLoanDate()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "loan date must be YYYY-MM-DD")
	}

	loanID := ulid.Make().String()
	loanName := strings.TrimSpace(req.GetLoanName())
	if loanName == "" {
		loanName = "房贷"
	}
	now := time.Now()
	item := &data.Loan{
		LoanID:           loanID,
		LoanName:         loanName,
		LoanType:         data.LoanTypeMortgage,
		LoanDate:         formatDate(loanDate),
		InitialPrincipal: formatMoney(principal),
		AnnualRate:       formatRate(annualRate),
		TermMonths:       int32(termYears * 12),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err = s.loanRepo.Create(ctx, item); err != nil {
		s.log.Errorf("create mortgage loan failed: %v", err)
		return nil, status.Error(codes.Internal, "create mortgage loan failed")
	}
	return &bookkeepingv1.CreateMortgageLoanReply{
		LoanId: loanID,
	}, nil
}

func (s *BookkeepingService) StartLoanRepayment(ctx context.Context, req *bookkeepingv1.StartLoanRepaymentRequest) (*bookkeepingv1.StartLoanRepaymentReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	loanID := strings.TrimSpace(req.GetLoanId())
	if loanID == "" {
		return nil, status.Error(codes.InvalidArgument, "loan id is required")
	}
	startDateRaw := strings.TrimSpace(req.GetStartDate())
	startDate, err := parseDate(startDateRaw)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "start date must be YYYY-MM-DD")
	}
	repaymentDay := int32(req.GetRepaymentDay())
	if repaymentDay < 1 || repaymentDay > 31 {
		return nil, status.Error(codes.InvalidArgument, "repayment day must be 1..31")
	}
	if _, err := s.loanRepo.FindByLoanID(ctx, loanID); err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Error(codes.NotFound, "loan not found")
		}
		s.log.Errorf("query loan failed: loan_id=%s err=%v", loanID, err)
		return nil, status.Error(codes.Internal, "query loan failed")
	}
	if err := s.loanRepo.UpdateRepaymentProfile(ctx, loanID, formatDate(startDate), repaymentDay, time.Now()); err != nil {
		s.log.Errorf("start repayment failed: loan_id=%s err=%v", loanID, err)
		return nil, status.Error(codes.Internal, "start repayment failed")
	}
	firstDue := firstDueDate(startDate, repaymentDay)
	return &bookkeepingv1.StartLoanRepaymentReply{
		LoanId:       loanID,
		StartDate:    formatDate(startDate),
		RepaymentDay: uint32(repaymentDay),
		FirstDueDate: formatDate(firstDue),
	}, nil
}

//nolint:cyclop,funlen // business validation covers multiple date/profile compatibility branches.
func (s *BookkeepingService) AdjustLoanRate(ctx context.Context, req *bookkeepingv1.AdjustLoanRateRequest) (*bookkeepingv1.AdjustLoanRateReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	loanID := strings.TrimSpace(req.GetLoanId())
	if loanID == "" {
		return nil, status.Error(codes.InvalidArgument, "loan id is required")
	}
	effectiveDateRaw := strings.TrimSpace(req.GetEffectiveDate())
	effectiveDate, err := parseDate(effectiveDateRaw)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "effective date must be YYYY-MM-DD")
	}
	annualRate, err := parseRate(req.GetAnnualRate())
	if err != nil || annualRate < 0 {
		return nil, status.Error(codes.InvalidArgument, "annual rate is invalid")
	}

	loan, err := s.loanRepo.FindByLoanID(ctx, loanID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Error(codes.NotFound, "loan not found")
		}
		s.log.Errorf("query loan failed: loan_id=%s err=%v", loanID, err)
		return nil, status.Error(codes.Internal, "query loan failed")
	}
	if strings.TrimSpace(loan.StartDate) != "" {
		startDate, parseErr := parseDate(loan.StartDate)
		if parseErr != nil {
			s.log.Errorf("loan start date invalid in db: loan_id=%s start_date=%s err=%v", loanID, loan.StartDate, parseErr)
			return nil, status.Error(codes.Internal, "loan data is invalid")
		}
		if effectiveDate.Before(startDate) {
			return nil, status.Error(codes.InvalidArgument, "effective date must be >= start date")
		}
	} else if strings.TrimSpace(loan.LoanDate) != "" {
		loanDate, parseErr := parseDate(loan.LoanDate)
		if parseErr != nil {
			s.log.Errorf("loan date invalid in db: loan_id=%s loan_date=%s err=%v", loanID, loan.LoanDate, parseErr)
			return nil, status.Error(codes.Internal, "loan data is invalid")
		}
		if effectiveDate.Before(loanDate) {
			return nil, status.Error(codes.InvalidArgument, "effective date must be >= loan date")
		}
	} else if loan.StartYM != "" {
		startYM, parseErr := parseYM(loan.StartYM)
		if parseErr != nil {
			s.log.Errorf("loan start ym invalid in db: loan_id=%s start_ym=%s err=%v", loanID, loan.StartYM, parseErr)
			return nil, status.Error(codes.Internal, "loan data is invalid")
		}
		if effectiveDate.Before(time.Date(startYM.year, time.Month(startYM.month), 1, 0, 0, 0, 0, time.UTC)) {
			return nil, status.Error(codes.InvalidArgument, "effective date must be >= start month")
		}
	}

	now := time.Now()
	item := &data.LoanRateAdjustment{
		LoanID:        loanID,
		EffectiveDate: formatDate(effectiveDate),
		EffectiveYM:   strings.ReplaceAll(formatDate(effectiveDate)[:7], "-", "."),
		AnnualRate:    formatRate(annualRate),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err = s.loanRepo.UpsertRateAdjustment(ctx, item); err != nil {
		s.log.Errorf("adjust loan rate failed: loan_id=%s err=%v", loanID, err)
		return nil, status.Error(codes.Internal, "adjust loan rate failed")
	}
	return &bookkeepingv1.AdjustLoanRateReply{
		LoanId:        loanID,
		EffectiveDate: formatDate(effectiveDate),
		AnnualRate:    formatRate(annualRate),
	}, nil
}

func (s *BookkeepingService) AddLoanPrepayment(ctx context.Context, req *bookkeepingv1.AddLoanPrepaymentRequest) (*bookkeepingv1.AddLoanPrepaymentReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	loanID := strings.TrimSpace(req.GetLoanId())
	if loanID == "" {
		return nil, status.Error(codes.InvalidArgument, "loan id is required")
	}
	prepayDate, err := parseDate(strings.TrimSpace(req.GetPrepaymentDate()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "prepayment date must be YYYY-MM-DD")
	}
	amount, err := parseMoney(req.GetAmount())
	if err != nil || amount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "amount must be positive")
	}
	mode, modeErr := fromProtoPrepaymentMode(req.GetMode())
	if modeErr != nil {
		return nil, status.Error(codes.InvalidArgument, modeErr.Error())
	}
	loan, err := s.loanRepo.FindByLoanID(ctx, loanID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Error(codes.NotFound, "loan not found")
		}
		s.log.Errorf("query loan failed: loan_id=%s err=%v", loanID, err)
		return nil, status.Error(codes.Internal, "query loan failed")
	}
	if strings.TrimSpace(loan.StartDate) != "" {
		startDate, parseErr := parseDate(loan.StartDate)
		if parseErr != nil {
			s.log.Errorf("loan start date invalid in db: loan_id=%s start_date=%s err=%v", loanID, loan.StartDate, parseErr)
			return nil, status.Error(codes.Internal, "loan data is invalid")
		}
		if prepayDate.Before(startDate) {
			return nil, status.Error(codes.InvalidArgument, "prepayment date must be >= start date")
		}
	}
	now := time.Now()
	item := &data.LoanPrepayment{
		LoanID:         loanID,
		PrepaymentDate: formatDate(prepayDate),
		Amount:         formatMoney(amount),
		Mode:           mode,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err = s.loanRepo.AddPrepayment(ctx, item); err != nil {
		s.log.Errorf("add loan prepayment failed: loan_id=%s err=%v", loanID, err)
		return nil, status.Error(codes.Internal, "add loan prepayment failed")
	}
	return &bookkeepingv1.AddLoanPrepaymentReply{
		LoanId:         loanID,
		PrepaymentDate: item.PrepaymentDate,
		Amount:         item.Amount,
		Mode:           toProtoPrepaymentMode(item.Mode),
	}, nil
}

func (s *BookkeepingService) LoanSummaryList(ctx context.Context, _ *bookkeepingv1.LoanSummaryListRequest) (*bookkeepingv1.LoanSummaryListReply, error) {
	lc, err := s.loadLoanContext(ctx, "")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	reply := &bookkeepingv1.LoanSummaryListReply{
		Items: make([]*bookkeepingv1.LoanSummaryItem, 0, len(lc.loans)),
	}
	for i := range lc.loans {
		comp := lc.computations[lc.loans[i].LoanID]
		if comp == nil {
			continue
		}
		summary, sumErr := comp.summaryAt(now)
		if sumErr != nil {
			s.log.Errorf("loan summary calc failed loan_id=%s err=%v", lc.loans[i].LoanID, sumErr)
			return nil, status.Error(codes.Internal, "loan summary calc failed")
		}
		reply.Items = append(reply.Items, &bookkeepingv1.LoanSummaryItem{
			LoanId:              lc.loans[i].LoanID,
			LoanName:            lc.loans[i].LoanName,
			LoanType:            toProtoLoanType(lc.loans[i].LoanType),
			LoanDate:            lc.loans[i].LoanDate,
			AnnualRate:          formatRate(summary.AnnualRate),
			InitialPrincipal:    lc.loans[i].InitialPrincipal,
			RemainingPrincipal:  formatMoney(summary.RemainingPrincipal),
			RepaidMonths:        uint32(summary.RepaidMonths),
			CumulativePrincipal: formatMoney(summary.CumulativePrincipal),
			CumulativeInterest:  formatMoney(summary.CumulativeInterest),
			NextDueDate:         summary.NextDueDate,
			RemainingInterest:   formatMoney(summary.RemainingInterest),
		})
	}
	return reply, nil
}

//nolint:cyclop,funlen // response aggregates summary/plans/rate-history/prepayment-history in one RPC.
func (s *BookkeepingService) LoanDetail(ctx context.Context, req *bookkeepingv1.LoanDetailRequest) (*bookkeepingv1.LoanDetailReply, error) {
	if req == nil || strings.TrimSpace(req.GetLoanId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "loan id is required")
	}
	lc, err := s.loadLoanContext(ctx, strings.TrimSpace(req.GetLoanId()))
	if err != nil {
		return nil, err
	}
	if len(lc.loans) == 0 {
		return nil, status.Error(codes.NotFound, "loan not found")
	}
	loan := lc.loans[0]
	comp := lc.computations[loan.LoanID]
	if comp == nil {
		return nil, status.Error(codes.Internal, "loan calc not found")
	}
	now := time.Now().UTC()
	summary, sumErr := comp.summaryAt(now)
	if sumErr != nil {
		s.log.Errorf("loan detail summary calc failed loan_id=%s err=%v", loan.LoanID, sumErr)
		return nil, status.Error(codes.Internal, "loan summary calc failed")
	}
	months := int(req.GetMonths())
	if months <= 0 {
		months = 24
	}
	if months > 120 {
		months = 120
	}
	plans := comp.futurePlans(now, months)
	baseRate, err := parseRate(loan.AnnualRate)
	if err != nil {
		s.log.Errorf("loan annual rate invalid loan_id=%s rate=%s err=%v", loan.LoanID, loan.AnnualRate, err)
		return nil, status.Error(codes.Internal, "loan data is invalid")
	}
	rateAdjustments := lc.adjustmentsByLoan[loan.LoanID]
	rateHistory := make([]*bookkeepingv1.LoanRateAdjustmentHistoryItem, 0, len(rateAdjustments))
	prevRate := baseRate
	for i := range rateAdjustments {
		newRate, parseErr := parseRate(rateAdjustments[i].AnnualRate)
		if parseErr != nil {
			s.log.Errorf("loan rate adjustment invalid loan_id=%s adjustment_rate=%s err=%v", loan.LoanID, rateAdjustments[i].AnnualRate, parseErr)
			return nil, status.Error(codes.Internal, "loan data is invalid")
		}
		deltaBps := round2((newRate - prevRate) * 10000)
		rateHistory = append(rateHistory, &bookkeepingv1.LoanRateAdjustmentHistoryItem{
			EffectiveDate:      formatDate(normalizeAdjustmentEffectiveDate(rateAdjustments[i])),
			PreviousAnnualRate: formatRate(prevRate),
			NewAnnualRate:      formatRate(newRate),
			DeltaBps:           fmt.Sprintf("%.2f", deltaBps),
		})
		prevRate = newRate
	}
	sort.Slice(rateHistory, func(i, j int) bool {
		return rateHistory[i].EffectiveDate > rateHistory[j].EffectiveDate
	})
	prepayments := lc.prepaymentsByLoan[loan.LoanID]
	prepaymentHistory := make([]*bookkeepingv1.LoanPrepaymentHistoryItem, 0, len(prepayments))
	totalWithPrepay := comp.totalPlannedInterest()
	for i := range prepayments {
		prepaymentsWithoutCurrent := make([]data.LoanPrepayment, 0, len(prepayments)-1)
		prepaymentsWithoutCurrent = append(prepaymentsWithoutCurrent, prepayments[:i]...)
		prepaymentsWithoutCurrent = append(prepaymentsWithoutCurrent, prepayments[i+1:]...)
		compWithoutCurrent, calcErr := buildLoanComputation(loan, lc.adjustmentsByLoan[loan.LoanID], prepaymentsWithoutCurrent)
		if calcErr != nil {
			s.log.Errorf("build loan computation for prepayment saving failed loan_id=%s idx=%d err=%v", loan.LoanID, i, calcErr)
			return nil, status.Error(codes.Internal, "loan schedule calc failed")
		}
		savedInterest := compWithoutCurrent.totalPlannedInterest() - totalWithPrepay
		if savedInterest < 0 {
			savedInterest = 0
		}
		prepaymentHistory = append(prepaymentHistory, &bookkeepingv1.LoanPrepaymentHistoryItem{
			PrepaymentDate: prepayments[i].PrepaymentDate,
			Amount:         prepayments[i].Amount,
			Mode:           toProtoPrepaymentMode(prepayments[i].Mode),
			SavedInterest:  formatMoney(savedInterest),
		})
	}
	sort.Slice(prepaymentHistory, func(i, j int) bool {
		return prepaymentHistory[i].PrepaymentDate > prepaymentHistory[j].PrepaymentDate
	})
	reply := &bookkeepingv1.LoanDetailReply{
		Summary: &bookkeepingv1.LoanSummaryItem{
			LoanId:              loan.LoanID,
			LoanName:            loan.LoanName,
			LoanType:            toProtoLoanType(loan.LoanType),
			LoanDate:            loan.LoanDate,
			AnnualRate:          formatRate(summary.AnnualRate),
			InitialPrincipal:    loan.InitialPrincipal,
			RemainingPrincipal:  formatMoney(summary.RemainingPrincipal),
			RepaidMonths:        uint32(summary.RepaidMonths),
			CumulativePrincipal: formatMoney(summary.CumulativePrincipal),
			CumulativeInterest:  formatMoney(summary.CumulativeInterest),
			NextDueDate:         summary.NextDueDate,
			RemainingInterest:   formatMoney(summary.RemainingInterest),
		},
		Plans:           make([]*bookkeepingv1.LoanRepaymentPlanItem, 0, len(plans)),
		RateAdjustments: rateHistory,
		Prepayments:     prepaymentHistory,
	}
	for i := range plans {
		reply.Plans = append(reply.Plans, &bookkeepingv1.LoanRepaymentPlanItem{
			Period:             uint32(plans[i].Period),
			DueDate:            formatDate(plans[i].DueDate),
			Ym:                 plans[i].YM,
			MonthlyPrincipal:   formatMoney(plans[i].MonthlyPrincipal),
			MonthlyInterest:    formatMoney(plans[i].MonthlyInterest),
			RemainingPrincipal: formatMoney(plans[i].RemainingPrincipal),
		})
	}
	return reply, nil
}

func toProtoLoanType(loanType string) bookkeepingv1.LoanType {
	switch loanType {
	case data.LoanTypeMortgage:
		return bookkeepingv1.LoanType_LOAN_TYPE_MORTGAGE
	default:
		return bookkeepingv1.LoanType_LOAN_TYPE_UNSPECIFIED
	}
}

func toProtoPrepaymentMode(mode string) bookkeepingv1.PrepaymentMode {
	switch mode {
	case data.PrepaymentModeKeepPaymentShortenTerm:
		return bookkeepingv1.PrepaymentMode_PREPAYMENT_MODE_KEEP_PAYMENT_SHORTEN_TERM
	case data.PrepaymentModeKeepTermReducePayment:
		return bookkeepingv1.PrepaymentMode_PREPAYMENT_MODE_KEEP_TERM_REDUCE_PAYMENT
	default:
		return bookkeepingv1.PrepaymentMode_PREPAYMENT_MODE_UNSPECIFIED
	}
}

func fromProtoPrepaymentMode(mode bookkeepingv1.PrepaymentMode) (string, error) {
	switch mode {
	case bookkeepingv1.PrepaymentMode_PREPAYMENT_MODE_KEEP_PAYMENT_SHORTEN_TERM:
		return data.PrepaymentModeKeepPaymentShortenTerm, nil
	case bookkeepingv1.PrepaymentMode_PREPAYMENT_MODE_KEEP_TERM_REDUCE_PAYMENT:
		return data.PrepaymentModeKeepTermReducePayment, nil
	default:
		return "", fmt.Errorf("prepayment mode is invalid")
	}
}

func formatRate(rate float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", rate), "0"), ".")
}

//nolint:cyclop,funlen // context loader normalizes data from multiple repositories for downstream RPCs.
func (s *BookkeepingService) loadLoanContext(ctx context.Context, loanID string) (*loanContext, error) {
	loans, err := s.loanRepo.List(ctx)
	if err != nil {
		s.log.Errorf("query loans failed: %v", err)
		return nil, status.Error(codes.Internal, "query loans failed")
	}
	if loanID != "" {
		filtered := make([]data.Loan, 0, 1)
		for i := range loans {
			if loans[i].LoanID == loanID {
				filtered = append(filtered, loans[i])
				break
			}
		}
		loans = filtered
	}
	if len(loans) == 0 {
		return nil, status.Error(codes.NotFound, "loan not found")
	}
	loanIDs := make([]string, 0, len(loans))
	for i := range loans {
		loanIDs = append(loanIDs, loans[i].LoanID)
	}
	adjustments, err := s.loanRepo.ListRateAdjustments(ctx, loanIDs)
	if err != nil {
		s.log.Errorf("query loan adjustments failed: %v", err)
		return nil, status.Error(codes.Internal, "query loan adjustments failed")
	}
	adjustmentsByLoanID := make(map[string][]data.LoanRateAdjustment, len(loanIDs))
	for i := range adjustments {
		adjustmentsByLoanID[adjustments[i].LoanID] = append(adjustmentsByLoanID[adjustments[i].LoanID], adjustments[i])
	}
	for id := range adjustmentsByLoanID {
		sort.Slice(adjustmentsByLoanID[id], func(i, j int) bool {
			di := normalizeAdjustmentEffectiveDate(adjustmentsByLoanID[id][i])
			dj := normalizeAdjustmentEffectiveDate(adjustmentsByLoanID[id][j])
			if di.Equal(dj) {
				return adjustmentsByLoanID[id][i].CreatedAt.Before(adjustmentsByLoanID[id][j].CreatedAt)
			}
			return di.Before(dj)
		})
	}
	prepayments, err := s.loanRepo.ListPrepayments(ctx, loanIDs)
	if err != nil {
		s.log.Errorf("query loan prepayments failed: %v", err)
		return nil, status.Error(codes.Internal, "query loan prepayments failed")
	}
	prepaymentsByLoanID := make(map[string][]data.LoanPrepayment, len(loanIDs))
	for i := range prepayments {
		prepaymentsByLoanID[prepayments[i].LoanID] = append(prepaymentsByLoanID[prepayments[i].LoanID], prepayments[i])
	}
	for id := range prepaymentsByLoanID {
		sort.Slice(prepaymentsByLoanID[id], func(i, j int) bool {
			di, _ := parseDate(prepaymentsByLoanID[id][i].PrepaymentDate)
			dj, _ := parseDate(prepaymentsByLoanID[id][j].PrepaymentDate)
			if di.Equal(dj) {
				return prepaymentsByLoanID[id][i].CreatedAt.Before(prepaymentsByLoanID[id][j].CreatedAt)
			}
			return di.Before(dj)
		})
	}
	computations := make(map[string]*loanComputation, len(loans))
	for i := range loans {
		comp, calcErr := buildLoanComputation(loans[i], adjustmentsByLoanID[loans[i].LoanID], prepaymentsByLoanID[loans[i].LoanID])
		if calcErr != nil {
			s.log.Errorf("build loan computation failed loan_id=%s err=%v", loans[i].LoanID, calcErr)
			return nil, status.Error(codes.Internal, "loan schedule calc failed")
		}
		computations[loans[i].LoanID] = comp
	}
	return &loanContext{
		loans:             loans,
		adjustmentsByLoan: adjustmentsByLoanID,
		prepaymentsByLoan: prepaymentsByLoanID,
		computations:      computations,
	}, nil
}

func normalizeAdjustmentEffectiveDate(adj data.LoanRateAdjustment) time.Time {
	if strings.TrimSpace(adj.EffectiveDate) != "" {
		d, err := parseDate(adj.EffectiveDate)
		if err == nil {
			return d
		}
	}
	if strings.TrimSpace(adj.EffectiveYM) != "" {
		ym, err := parseYM(adj.EffectiveYM)
		if err == nil {
			return time.Date(ym.year, time.Month(ym.month), 1, 0, 0, 0, 0, time.UTC)
		}
	}
	return time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)
}
