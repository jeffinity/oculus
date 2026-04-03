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

	assetRepo        *data.AssetRepo
	assetDetailRepo  *data.AssetDetailRepo
	loanRepo         *data.LoanRepo
	consumerLoanRepo *data.ConsumerLoanRepo
	ledgerRepo       *data.LedgerRepo
	log              *log.Helper
}

type loanContext struct {
	loans             []data.Loan
	adjustmentsByLoan map[string][]data.LoanRateAdjustment
	prepaymentsByLoan map[string][]data.LoanPrepayment
	computations      map[string]*loanComputation
}

type consumerLoanPayload struct {
	totalAmount float64
	startYM     string
	termMonths  int32
}

var supportedLedgerCategories = map[string]struct{}{
	"餐饮": {}, "购物": {}, "日用": {}, "交通": {}, "汽车": {}, "蔬菜": {}, "水果": {}, "零食": {},
	"运动": {}, "娱乐": {}, "通讯": {}, "服饰": {}, "美容": {}, "住房": {}, "居家": {}, "孩子": {},
	"长辈": {}, "社交": {}, "旅行": {}, "烟酒": {}, "数码": {}, "医疗": {}, "书籍": {}, "学习": {},
	"宠物": {}, "礼金": {}, "礼物": {}, "办公": {}, "维修": {}, "捐赠": {}, "彩票": {}, "亲友": {},
	"快递": {}, "设置": {},
}

var supportedAssetTypes = map[string]struct{}{
	"asset":     {},
	"liability": {},
}

var supportedAssetSubTypes = map[string]struct{}{
	"现金":   {},
	"储蓄卡":  {},
	"虚拟账户": {},
	"债权":   {},
	"信用卡":  {},
	"欠款":   {},
	"消费贷款": {},
}

var assetDetailStartYM = ymValue{year: 2026, month: 4}

func NewBookkeepingService(
	assetRepo *data.AssetRepo,
	assetDetailRepo *data.AssetDetailRepo,
	loanRepo *data.LoanRepo,
	consumerLoanRepo *data.ConsumerLoanRepo,
	ledgerRepo *data.LedgerRepo,
	logger log.Logger,
) *BookkeepingService {
	return &BookkeepingService{
		assetRepo:        assetRepo,
		assetDetailRepo:  assetDetailRepo,
		loanRepo:         loanRepo,
		consumerLoanRepo: consumerLoanRepo,
		ledgerRepo:       ledgerRepo,
		log:              log.NewHelper(log.With(logger, "module", "bookkeeping/BookkeepingService")),
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

func (s *BookkeepingService) AssetDetailList(ctx context.Context, req *bookkeepingv1.AssetDetailListRequest) (*bookkeepingv1.AssetDetailListReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ym := strings.TrimSpace(req.GetYm())
	targetYM, err := parseYM(ym)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "ym must be YYYY.MM")
	}
	if targetYM.Before(assetDetailStartYM) {
		return &bookkeepingv1.AssetDetailListReply{Items: []*bookkeepingv1.AssetDetailItem{}}, nil
	}

	items, err := s.assetDetailRepo.ListByYM(ctx, ym)
	if err != nil {
		s.log.Errorf("query asset details failed: ym=%s err=%v", ym, err)
		return nil, status.Error(codes.Internal, "query asset details failed")
	}
	if len(items) == 0 {
		currentYM := ymFromDate(time.Now())
		if targetYM.String() == currentYM.String() {
			prevYM, findErr := s.assetDetailRepo.FindLatestYMBefore(ctx, ym)
			if findErr != nil {
				s.log.Errorf("query previous asset detail month failed: ym=%s err=%v", ym, findErr)
				return nil, status.Error(codes.Internal, "query asset details failed")
			}
			if prevYM != "" {
				prevItems, listErr := s.assetDetailRepo.ListByYM(ctx, prevYM)
				if listErr != nil {
					s.log.Errorf("query previous asset details failed: ym=%s prev_ym=%s err=%v", ym, prevYM, listErr)
					return nil, status.Error(codes.Internal, "query asset details failed")
				}
				now := time.Now()
				for i := range prevItems {
					if prevItems[i].SubType == "消费贷款" {
						continue
					}
					createErr := s.assetDetailRepo.Create(ctx, &data.AssetDetail{
						DetailID:   ulid.Make().String(),
						YM:         ym,
						AssetType:  prevItems[i].AssetType,
						SubType:    prevItems[i].SubType,
						PresetCode: prevItems[i].PresetCode,
						AssetName:  prevItems[i].AssetName,
						Remark:     prevItems[i].Remark,
						Amount:     prevItems[i].Amount,
						CreatedAt:  now,
						UpdatedAt:  now,
					})
					if createErr != nil {
						s.log.Errorf("copy asset details to current month failed: ym=%s prev_ym=%s err=%v", ym, prevYM, createErr)
						return nil, status.Error(codes.Internal, "query asset details failed")
					}
				}
				items, err = s.assetDetailRepo.ListByYM(ctx, ym)
				if err != nil {
					s.log.Errorf("query copied asset details failed: ym=%s err=%v", ym, err)
					return nil, status.Error(codes.Internal, "query asset details failed")
				}
			}
		}
	}
	if err = s.syncAssetSummaryByYM(ctx, ym); err != nil {
		s.log.Errorf("sync asset summary failed: ym=%s err=%v", ym, err)
		return nil, status.Error(codes.Internal, "sync asset summary failed")
	}

	consumerByDetailID, buildErr := s.loadConsumerLoanMapForYM(ctx, targetYM)
	if buildErr != nil {
		s.log.Errorf("load consumer loans failed: ym=%s err=%v", ym, buildErr)
		return nil, status.Error(codes.Internal, "query asset details failed")
	}

	reply := &bookkeepingv1.AssetDetailListReply{
		Items: make([]*bookkeepingv1.AssetDetailItem, 0, len(items)+len(consumerByDetailID)),
	}
	for i := range items {
		if items[i].SubType == "消费贷款" {
			if payload, ok := consumerByDetailID[items[i].DetailID]; ok {
				reply.Items = append(reply.Items, toProtoAssetDetailItem(items[i], &consumerLoanPayload{
					totalAmount: payload.totalAmount,
					startYM:     payload.startYM,
					termMonths:  payload.termMonths,
				}))
				delete(consumerByDetailID, items[i].DetailID)
			}
			continue
		}
		reply.Items = append(reply.Items, toProtoAssetDetailItem(items[i], nil))
	}
	for _, payload := range consumerByDetailID {
		reply.Items = append(reply.Items, toProtoAssetDetailItem(data.AssetDetail{
			DetailID:   payload.DetailID,
			YM:         ym,
			AssetType:  "liability",
			SubType:    "消费贷款",
			PresetCode: payload.PresetCode,
			AssetName:  payload.AssetName,
			Remark:     payload.Remark,
			Amount:     formatMoney(payload.remainingAmount),
			CreatedAt:  payload.CreatedAt,
			UpdatedAt:  payload.UpdatedAt,
		}, &consumerLoanPayload{
			totalAmount: payload.totalAmount,
			startYM:     payload.startYM,
			termMonths:  payload.termMonths,
		}))
	}
	sort.Slice(reply.Items, func(i, j int) bool {
		if reply.Items[i].AssetType != reply.Items[j].AssetType {
			return reply.Items[i].AssetType < reply.Items[j].AssetType
		}
		if reply.Items[i].SubType != reply.Items[j].SubType {
			return reply.Items[i].SubType < reply.Items[j].SubType
		}
		if reply.Items[i].DetailId != reply.Items[j].DetailId {
			return reply.Items[i].DetailId < reply.Items[j].DetailId
		}
		return reply.Items[i].AssetName < reply.Items[j].AssetName
	})
	return reply, nil
}

func (s *BookkeepingService) CreateAssetDetail(ctx context.Context, req *bookkeepingv1.CreateAssetDetailRequest) (*bookkeepingv1.CreateAssetDetailReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	rawAmount := req.GetAmount()
	if strings.TrimSpace(req.GetSubType()) == "消费贷款" && strings.TrimSpace(rawAmount) == "" {
		rawAmount = req.GetConsumerLoanTotalAmount()
	}
	ym, assetType, subType, presetCode, assetName, remark, amount, err := normalizeAssetDetailPayload(
		req.GetYm(),
		req.GetAssetType(),
		req.GetSubType(),
		req.GetPresetCode(),
		req.GetAssetName(),
		req.GetRemark(),
		rawAmount,
	)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	var consumerPayload *consumerLoanPayload
	if subType == "消费贷款" {
		consumerPayload, err = normalizeConsumerLoanPayload(req.GetConsumerLoanTotalAmount(), req.GetConsumerLoanStartYm(), req.GetConsumerLoanTermMonths())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		ym = consumerPayload.startYM
		assetType = "liability"
		amount = consumerPayload.totalAmount
	}

	now := time.Now()
	item := data.AssetDetail{
		DetailID:   ulid.Make().String(),
		YM:         ym,
		AssetType:  assetType,
		SubType:    subType,
		PresetCode: presetCode,
		AssetName:  assetName,
		Remark:     remark,
		Amount:     formatMoney(amount),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err = s.assetDetailRepo.Create(ctx, &item); err != nil {
		s.log.Errorf("create asset detail failed: ym=%s sub_type=%s err=%v", ym, subType, err)
		return nil, status.Error(codes.Internal, "create asset detail failed")
	}
	if consumerPayload != nil {
		if err = s.consumerLoanRepo.Upsert(ctx, &data.ConsumerLoan{
			DetailID:    item.DetailID,
			TotalAmount: formatMoney(consumerPayload.totalAmount),
			StartYM:     consumerPayload.startYM,
			TermMonths:  consumerPayload.termMonths,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			s.log.Errorf("create consumer loan failed: detail_id=%s err=%v", item.DetailID, err)
			return nil, status.Error(codes.Internal, "create asset detail failed")
		}
	}
	if err = s.syncAssetSummaryByYM(ctx, ym); err != nil {
		s.log.Errorf("sync asset summary after create failed: ym=%s err=%v", ym, err)
		return nil, status.Error(codes.Internal, "sync asset summary failed")
	}
	if consumerPayload != nil {
		if err = s.resyncAssetSummaryFromYM(ctx, ym); err != nil {
			s.log.Errorf("resync consumer loan summary failed: start_ym=%s err=%v", ym, err)
			return nil, status.Error(codes.Internal, "sync asset summary failed")
		}
	}
	return &bookkeepingv1.CreateAssetDetailReply{
		Item: toProtoAssetDetailItem(item, consumerPayload),
	}, nil
}

func (s *BookkeepingService) UpdateAssetDetail(ctx context.Context, req *bookkeepingv1.UpdateAssetDetailRequest) (*bookkeepingv1.UpdateAssetDetailReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	detailID := strings.TrimSpace(req.GetDetailId())
	if detailID == "" {
		return nil, status.Error(codes.InvalidArgument, "detail id is required")
	}
	existing, err := s.assetDetailRepo.FindByID(ctx, detailID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Error(codes.NotFound, "asset detail not found")
		}
		s.log.Errorf("query asset detail failed: detail_id=%s err=%v", detailID, err)
		return nil, status.Error(codes.Internal, "query asset detail failed")
	}

	rawYM := strings.TrimSpace(req.GetYm())
	if rawYM == "" {
		rawYM = existing.YM
	}
	rawAssetType := strings.TrimSpace(req.GetAssetType())
	if rawAssetType == "" {
		rawAssetType = existing.AssetType
	}
	rawSubType := strings.TrimSpace(req.GetSubType())
	if rawSubType == "" {
		rawSubType = existing.SubType
	}
	rawPresetCode := strings.TrimSpace(req.GetPresetCode())
	if rawPresetCode == "" {
		rawPresetCode = existing.PresetCode
	}
	rawAmount := strings.TrimSpace(req.GetAmount())
	if rawSubType == "消费贷款" && rawAmount == "" {
		rawAmount = strings.TrimSpace(req.GetConsumerLoanTotalAmount())
	}
	if rawAmount == "" {
		rawAmount = existing.Amount
	}
	ym, assetType, subType, presetCode, assetName, remark, amount, err := normalizeAssetDetailPayload(
		rawYM,
		rawAssetType,
		rawSubType,
		rawPresetCode,
		req.GetAssetName(),
		req.GetRemark(),
		rawAmount,
	)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	existingConsumer, _ := s.consumerLoanRepo.FindByDetailID(ctx, detailID)
	var consumerPayload *consumerLoanPayload
	if subType == "消费贷款" {
		consumerPayload, err = normalizeConsumerLoanPayload(req.GetConsumerLoanTotalAmount(), req.GetConsumerLoanStartYm(), req.GetConsumerLoanTermMonths())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		ym = consumerPayload.startYM
		assetType = "liability"
		amount = consumerPayload.totalAmount
	}

	item := data.AssetDetail{
		DetailID:   existing.DetailID,
		YM:         ym,
		AssetType:  assetType,
		SubType:    subType,
		PresetCode: presetCode,
		AssetName:  assetName,
		Remark:     remark,
		Amount:     formatMoney(amount),
		CreatedAt:  existing.CreatedAt,
		UpdatedAt:  time.Now(),
	}
	if err = s.assetDetailRepo.Update(ctx, &item); err != nil {
		s.log.Errorf("update asset detail failed: detail_id=%s err=%v", detailID, err)
		return nil, status.Error(codes.Internal, "update asset detail failed")
	}
	if consumerPayload != nil {
		if err = s.consumerLoanRepo.Upsert(ctx, &data.ConsumerLoan{
			DetailID:    detailID,
			TotalAmount: formatMoney(consumerPayload.totalAmount),
			StartYM:     consumerPayload.startYM,
			TermMonths:  consumerPayload.termMonths,
			CreatedAt:   existing.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		}); err != nil {
			s.log.Errorf("upsert consumer loan failed: detail_id=%s err=%v", detailID, err)
			return nil, status.Error(codes.Internal, "update asset detail failed")
		}
	} else if existingConsumer != nil {
		if err = s.consumerLoanRepo.DeleteByDetailID(ctx, detailID); err != nil {
			s.log.Errorf("delete consumer loan failed: detail_id=%s err=%v", detailID, err)
			return nil, status.Error(codes.Internal, "update asset detail failed")
		}
	}
	if err = s.syncAssetSummaryByYM(ctx, existing.YM); err != nil {
		s.log.Errorf("sync asset summary for old month failed: ym=%s detail_id=%s err=%v", existing.YM, detailID, err)
		return nil, status.Error(codes.Internal, "sync asset summary failed")
	}
	if ym != existing.YM {
		if err = s.syncAssetSummaryByYM(ctx, ym); err != nil {
			s.log.Errorf("sync asset summary for new month failed: ym=%s detail_id=%s err=%v", ym, detailID, err)
			return nil, status.Error(codes.Internal, "sync asset summary failed")
		}
	}
	if consumerPayload != nil || existingConsumer != nil {
		minYM := existing.YM
		if consumerPayload != nil && consumerPayload.startYM < minYM {
			minYM = consumerPayload.startYM
		}
		if existingConsumer != nil && existingConsumer.StartYM < minYM {
			minYM = existingConsumer.StartYM
		}
		if err = s.resyncAssetSummaryFromYM(ctx, minYM); err != nil {
			s.log.Errorf("resync consumer loan summary failed: detail_id=%s min_ym=%s err=%v", detailID, minYM, err)
			return nil, status.Error(codes.Internal, "sync asset summary failed")
		}
	}
	return &bookkeepingv1.UpdateAssetDetailReply{
		Item: toProtoAssetDetailItem(item, consumerPayload),
	}, nil
}

func (s *BookkeepingService) DeleteAssetDetail(ctx context.Context, req *bookkeepingv1.DeleteAssetDetailRequest) (*bookkeepingv1.DeleteAssetDetailReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	detailID := strings.TrimSpace(req.GetDetailId())
	if detailID == "" {
		return nil, status.Error(codes.InvalidArgument, "detail id is required")
	}
	existing, err := s.assetDetailRepo.FindByID(ctx, detailID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Error(codes.NotFound, "asset detail not found")
		}
		s.log.Errorf("query asset detail failed: detail_id=%s err=%v", detailID, err)
		return nil, status.Error(codes.Internal, "query asset detail failed")
	}
	existingConsumer, _ := s.consumerLoanRepo.FindByDetailID(ctx, detailID)

	if err = s.assetDetailRepo.DeleteByID(ctx, detailID); err != nil {
		s.log.Errorf("delete asset detail failed: detail_id=%s err=%v", detailID, err)
		return nil, status.Error(codes.Internal, "delete asset detail failed")
	}
	if existingConsumer != nil {
		if err = s.consumerLoanRepo.DeleteByDetailID(ctx, detailID); err != nil {
			s.log.Errorf("delete consumer loan failed: detail_id=%s err=%v", detailID, err)
			return nil, status.Error(codes.Internal, "delete asset detail failed")
		}
	}
	if err = s.syncAssetSummaryByYM(ctx, existing.YM); err != nil {
		s.log.Errorf("sync asset summary after delete failed: ym=%s detail_id=%s err=%v", existing.YM, detailID, err)
		return nil, status.Error(codes.Internal, "sync asset summary failed")
	}
	if existingConsumer != nil {
		if err = s.resyncAssetSummaryFromYM(ctx, existingConsumer.StartYM); err != nil {
			s.log.Errorf("resync consumer loan summary failed after delete: detail_id=%s start_ym=%s err=%v", detailID, existingConsumer.StartYM, err)
			return nil, status.Error(codes.Internal, "sync asset summary failed")
		}
	}
	return &bookkeepingv1.DeleteAssetDetailReply{Success: true}, nil
}

func (s *BookkeepingService) UpdateAssetRemark(ctx context.Context, req *bookkeepingv1.UpdateAssetRemarkRequest) (*bookkeepingv1.UpdateAssetRemarkReply, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ym := strings.TrimSpace(req.GetYm())
	if _, err := parseYM(ym); err != nil {
		return nil, status.Error(codes.InvalidArgument, "ym must be YYYY.MM")
	}
	remark := strings.TrimSpace(req.GetRemark())

	existing, err := s.assetRepo.FindByYM(ctx, ym)
	if err != nil {
		if err != sql.ErrNoRows {
			s.log.Errorf("query asset summary failed: ym=%s err=%v", ym, err)
			return nil, status.Error(codes.Internal, "query asset summary failed")
		}

		if syncErr := s.syncAssetSummaryByYM(ctx, ym); syncErr != nil {
			s.log.Errorf("sync asset summary before remark update failed: ym=%s err=%v", ym, syncErr)
			return nil, status.Error(codes.Internal, "sync asset summary failed")
		}
		existing, err = s.assetRepo.FindByYM(ctx, ym)
		if err != nil && err != sql.ErrNoRows {
			s.log.Errorf("query synced asset summary failed: ym=%s err=%v", ym, err)
			return nil, status.Error(codes.Internal, "query asset summary failed")
		}
	}

	next := &data.Asset{
		YM:        ym,
		Asset:     "0",
		NetAsset:  "0",
		Liability: "0",
		Remark:    remark,
	}
	if existing != nil {
		next.Asset = existing.Asset
		next.NetAsset = existing.NetAsset
		next.Liability = existing.Liability
	}
	if err = s.assetRepo.Upsert(ctx, next); err != nil {
		s.log.Errorf("upsert asset remark failed: ym=%s err=%v", ym, err)
		return nil, status.Error(codes.Internal, "update asset remark failed")
	}

	return &bookkeepingv1.UpdateAssetRemarkReply{
		Item: &bookkeepingv1.AssetItem{
			Ym:        next.YM,
			Asset:     next.Asset,
			NetAsset:  next.NetAsset,
			Liability: next.Liability,
			Remark:    next.Remark,
		},
	}, nil
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
			EstimatedPayoffDate: summary.EstimatedPayoffDate,
			TermMonths:          uint32(lc.loans[i].TermMonths),
			ShortenedMonths:     uint32(summary.ShortenedMonths),
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
			EstimatedPayoffDate: summary.EstimatedPayoffDate,
			TermMonths:          uint32(loan.TermMonths),
			ShortenedMonths:     uint32(summary.ShortenedMonths),
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

func toProtoAssetDetailItem(item data.AssetDetail, consumerLoan *consumerLoanPayload) *bookkeepingv1.AssetDetailItem {
	reply := &bookkeepingv1.AssetDetailItem{
		DetailId:   item.DetailID,
		Ym:         item.YM,
		AssetType:  item.AssetType,
		SubType:    item.SubType,
		PresetCode: item.PresetCode,
		AssetName:  item.AssetName,
		Remark:     item.Remark,
		Amount:     item.Amount,
	}
	if consumerLoan != nil {
		reply.ConsumerLoanTotalAmount = formatMoney(consumerLoan.totalAmount)
		reply.ConsumerLoanStartYm = consumerLoan.startYM
		reply.ConsumerLoanTermMonths = uint32(consumerLoan.termMonths)
	}
	return reply
}

func normalizeAssetDetailPayload(
	rawYM, rawAssetType, rawSubType, rawPresetCode, rawAssetName, rawRemark, rawAmount string,
) (string, string, string, string, string, string, float64, error) {
	ym := strings.TrimSpace(rawYM)
	if _, err := parseYM(ym); err != nil {
		return "", "", "", "", "", "", 0, fmt.Errorf("ym must be YYYY.MM")
	}
	assetType := strings.ToLower(strings.TrimSpace(rawAssetType))
	if _, ok := supportedAssetTypes[assetType]; !ok {
		return "", "", "", "", "", "", 0, fmt.Errorf("asset type is unsupported")
	}
	subType := strings.TrimSpace(rawSubType)
	if _, ok := supportedAssetSubTypes[subType]; !ok {
		return "", "", "", "", "", "", 0, fmt.Errorf("sub type is unsupported")
	}
	assetName := strings.TrimSpace(rawAssetName)
	if assetName == "" {
		return "", "", "", "", "", "", 0, fmt.Errorf("asset name is required")
	}
	amount, err := parseMoney(rawAmount)
	if err != nil || amount <= 0 {
		return "", "", "", "", "", "", 0, fmt.Errorf("amount must be positive")
	}
	presetCode := strings.TrimSpace(rawPresetCode)
	remark := strings.TrimSpace(rawRemark)
	return ym, assetType, subType, presetCode, assetName, remark, amount, nil
}

func normalizeConsumerLoanPayload(rawTotalAmount, rawStartYM string, rawTermMonths uint32) (*consumerLoanPayload, error) {
	totalAmount, err := parseMoney(rawTotalAmount)
	if err != nil || totalAmount <= 0 {
		return nil, fmt.Errorf("consumer loan total amount must be positive")
	}
	startYM := strings.TrimSpace(rawStartYM)
	if _, err = parseYM(startYM); err != nil {
		return nil, fmt.Errorf("consumer loan start ym must be YYYY.MM")
	}
	termMonths := int32(rawTermMonths)
	if _, ok := supportedConsumerLoanTerms[termMonths]; !ok {
		return nil, fmt.Errorf("consumer loan term months is unsupported")
	}
	return &consumerLoanPayload{
		totalAmount: round2(totalAmount),
		startYM:     startYM,
		termMonths:  termMonths,
	}, nil
}

type consumerLoanMonthView struct {
	DetailID        string
	AssetName       string
	Remark          string
	PresetCode      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	totalAmount     float64
	startYM         string
	termMonths      int32
	remainingAmount float64
}

func (s *BookkeepingService) loadConsumerLoanMapForYM(ctx context.Context, targetYM ymValue) (map[string]consumerLoanMonthView, error) {
	records, err := s.consumerLoanRepo.ListWithDetail(ctx)
	if err != nil {
		return nil, err
	}
	reply := make(map[string]consumerLoanMonthView, len(records))
	for i := range records {
		startYM, parseErr := parseYM(records[i].StartYM)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid consumer loan start ym detail_id=%s", records[i].DetailID)
		}
		total, parseErr := parseMoney(records[i].TotalAmount)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid consumer loan total amount detail_id=%s", records[i].DetailID)
		}
		remaining, include := calcConsumerLoanRemaining(total, startYM, records[i].TermMonths, targetYM)
		if !include {
			continue
		}
		reply[records[i].DetailID] = consumerLoanMonthView{
			DetailID:        records[i].DetailID,
			AssetName:       records[i].AssetName,
			Remark:          records[i].Remark,
			PresetCode:      records[i].PresetCode,
			CreatedAt:       records[i].CreatedAt,
			UpdatedAt:       records[i].UpdatedAt,
			totalAmount:     round2(total),
			startYM:         records[i].StartYM,
			termMonths:      records[i].TermMonths,
			remainingAmount: remaining,
		}
	}
	return reply, nil
}

func (s *BookkeepingService) syncAssetSummaryByYM(ctx context.Context, ym string) error {
	targetYM, err := parseYM(ym)
	if err != nil {
		return err
	}
	if targetYM.Before(assetDetailStartYM) {
		return nil
	}
	items, err := s.assetDetailRepo.ListByYM(ctx, ym)
	if err != nil {
		return err
	}

	assetTotal := 0.0
	liabilityTotal := 0.0
	for i := range items {
		if items[i].SubType == "消费贷款" {
			continue
		}
		amount, parseErr := parseMoney(items[i].Amount)
		if parseErr != nil || amount < 0 {
			return fmt.Errorf("invalid amount in asset detail detail_id=%s", items[i].DetailID)
		}
		if items[i].AssetType == "liability" {
			liabilityTotal += amount
			continue
		}
		assetTotal += amount
	}
	consumerByDetailID, err := s.loadConsumerLoanMapForYM(ctx, targetYM)
	if err != nil {
		return err
	}
	for _, item := range consumerByDetailID {
		liabilityTotal += item.remainingAmount
	}
	remark := ""
	existing, err := s.assetRepo.FindByYM(ctx, ym)
	if err == nil {
		remark = existing.Remark
	} else if err != sql.ErrNoRows {
		return err
	}

	return s.assetRepo.Upsert(ctx, &data.Asset{
		YM:        ym,
		Asset:     formatMoney(assetTotal),
		NetAsset:  formatMoney(assetTotal - liabilityTotal),
		Liability: formatMoney(liabilityTotal),
		Remark:    remark,
	})
}

func (s *BookkeepingService) resyncAssetSummaryFromYM(ctx context.Context, startYM string) error {
	start, err := parseYM(startYM)
	if err != nil {
		return err
	}
	if start.Before(assetDetailStartYM) {
		start = assetDetailStartYM
	}
	items, err := s.assetRepo.List(ctx, start.String(), "")
	if err != nil {
		return err
	}
	for i := range items {
		if err = s.syncAssetSummaryByYM(ctx, items[i].YM); err != nil {
			return err
		}
	}
	return nil
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
