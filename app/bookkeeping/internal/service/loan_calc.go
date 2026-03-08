package service

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/data"
)

var ymRegex = regexp.MustCompile(`^\d{4}\.\d{2}$`)

type ymValue struct {
	year  int
	month int
}

type loanMonthSnapshot struct {
	RemainingPrincipal  float64
	MonthlyPrincipal    float64
	MonthlyInterest     float64
	PrepaymentPrincipal float64
	Include             bool
}

func parseYM(raw string) (ymValue, error) {
	ym := strings.TrimSpace(raw)
	if !ymRegex.MatchString(ym) {
		return ymValue{}, fmt.Errorf("invalid ym format: %s", raw)
	}
	year, err := strconv.Atoi(ym[:4])
	if err != nil {
		return ymValue{}, err
	}
	month, err := strconv.Atoi(ym[5:])
	if err != nil {
		return ymValue{}, err
	}
	if month < 1 || month > 12 {
		return ymValue{}, fmt.Errorf("invalid month: %d", month)
	}
	return ymValue{year: year, month: month}, nil
}

func (y ymValue) String() string {
	return fmt.Sprintf("%04d.%02d", y.year, y.month)
}

func (y ymValue) Before(other ymValue) bool {
	if y.year != other.year {
		return y.year < other.year
	}
	return y.month < other.month
}

func (y ymValue) AddMonths(n int) ymValue {
	abs := y.year*12 + (y.month - 1) + n
	year := abs / 12
	month := abs%12 + 1
	return ymValue{year: year, month: month}
}

func ymFromDate(d time.Time) ymValue {
	return ymValue{year: d.Year(), month: int(d.Month())}
}

func parseMoney(raw string) (float64, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if value == "" {
		return 0, nil
	}
	return strconv.ParseFloat(value, 64)
}

func parseRate(raw string) (float64, error) {
	n, err := parseMoney(raw)
	if err != nil {
		return 0, err
	}
	if n > 1 {
		n = n / 100
	}
	if n >= 1 {
		return 0, fmt.Errorf("annual rate too large: %s", raw)
	}
	return n, nil
}

func parseDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("date is empty")
	}
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "/", "-")
	d, err := time.Parse("2006-1-2", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date format: %s", raw)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC), nil
}

func formatDate(d time.Time) string {
	return d.Format("2006-01-02")
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func formatMoney(v float64) string {
	return strconv.FormatFloat(round2(v), 'f', 2, 64)
}

func annuityPayment(principal, annualRate float64, months int) float64 {
	if principal <= 0 || months <= 0 {
		return 0
	}
	if annualRate <= 0 {
		return principal / float64(months)
	}
	monthlyRate := annualRate / 12
	powValue := math.Pow(1+monthlyRate, float64(months))
	if powValue == 1 {
		return principal / float64(months)
	}
	return principal * monthlyRate * powValue / (powValue - 1)
}

func daysBetween(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	return int(end.Sub(start).Hours() / 24)
}

func dueDateOfMonth(year int, month time.Month, repaymentDay int32) time.Time {
	day := int(repaymentDay)
	if day < 1 {
		day = 1
	}
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func firstDueDate(startDate time.Time, repaymentDay int32) time.Time {
	nextMonth := time.Date(startDate.Year(), startDate.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	return dueDateOfMonth(nextMonth.Year(), nextMonth.Month(), repaymentDay)
}

func monthEnd(ym ymValue) time.Time {
	first := time.Date(ym.year, time.Month(ym.month), 1, 0, 0, 0, 0, time.UTC)
	return first.AddDate(0, 1, 0)
}

func normalizeRepaymentDay(day int32) int32 {
	if day < 1 {
		return 1
	}
	if day > 31 {
		return 31
	}
	return day
}

func startDateFromLoan(loan data.Loan) (time.Time, error) {
	if strings.TrimSpace(loan.StartDate) != "" {
		return parseDate(loan.StartDate)
	}
	if strings.TrimSpace(loan.StartYM) != "" {
		ym, err := parseYM(loan.StartYM)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(ym.year, time.Month(ym.month), 1, 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("start date is empty")
}

func loanDateFromLoan(loan data.Loan) (time.Time, error) {
	if strings.TrimSpace(loan.LoanDate) != "" {
		return parseDate(loan.LoanDate)
	}
	return startDateFromLoan(loan)
}

func repaymentDayFromLoan(loan data.Loan, startDate time.Time) int32 {
	if loan.RepaymentDay > 0 {
		return normalizeRepaymentDay(loan.RepaymentDay)
	}
	return int32(startDate.Day())
}

func firstPeriodExtraInterestDays(startDate time.Time, repaymentDay int32) int {
	firstAnchor := dueDateOfMonth(startDate.Year(), startDate.Month(), repaymentDay)
	if !firstAnchor.After(startDate) {
		return 0
	}
	// 首期补差统一按算头算尾。
	return daysBetween(startDate, firstAnchor) + 1
}

func carryoverSpreadDaysByDate(prevDue time.Time, effectiveDate time.Time) int {
	if !effectiveDate.After(prevDue) {
		return 0
	}
	// 按银行常见口径，补差计息采用算头算尾。
	return daysBetween(prevDue, effectiveDate) + 1
}

func loanSnapshotForYM(loan data.Loan, adjustments []data.LoanRateAdjustment, prepayments []data.LoanPrepayment, targetRawYM string) (loanMonthSnapshot, error) {
	c, err := buildLoanComputation(loan, adjustments, prepayments)
	if err != nil {
		return loanMonthSnapshot{}, err
	}
	return c.snapshotForYM(targetRawYM)
}

type loanRepaymentRecord struct {
	Period              int
	DueDate             time.Time
	YM                  string
	MonthlyPrincipal    float64
	MonthlyInterest     float64
	PrepaymentPrincipal float64
	RemainingPrincipal  float64
	CumulativePrincipal float64
	CumulativeInterest  float64
}

type normalizedAdjustment struct {
	effectiveDate time.Time
	rate          float64
}

type normalizedPrepayment struct {
	date      time.Time
	amount    float64
	recalcEMI bool
}

type loanComputation struct {
	loan          data.Loan
	loanDate      time.Time
	startDate     time.Time
	hasRepayment  bool
	initialAmount float64
	records       []loanRepaymentRecord
	rateTimeline  []normalizedAdjustment
	prepayments   []normalizedPrepayment
}

func buildLoanComputation(loan data.Loan, adjustments []data.LoanRateAdjustment, prepayments []data.LoanPrepayment) (*loanComputation, error) {
	principal, err := parseMoney(loan.InitialPrincipal)
	if err != nil {
		return nil, fmt.Errorf("parse principal failed: %w", err)
	}
	loanDate, err := loanDateFromLoan(loan)
	if err != nil {
		return nil, fmt.Errorf("invalid loan date: %w", err)
	}
	c := &loanComputation{
		loan:          loan,
		loanDate:      loanDate,
		initialAmount: round2(principal),
		records:       []loanRepaymentRecord{},
	}
	if principal <= 0 || loan.TermMonths <= 0 {
		return c, nil
	}

	hasRepaymentProfile := strings.TrimSpace(loan.StartDate) != "" || strings.TrimSpace(loan.StartYM) != ""
	if !hasRepaymentProfile {
		return c, nil
	}
	startDate, err := startDateFromLoan(loan)
	if err != nil {
		return nil, fmt.Errorf("invalid start date: %w", err)
	}
	c.startDate = startDate
	c.hasRepayment = true

	baseRate, err := parseRate(loan.AnnualRate)
	if err != nil {
		return nil, fmt.Errorf("parse base rate failed: %w", err)
	}
	c.rateTimeline = make([]normalizedAdjustment, 0, len(adjustments))
	for i := range adjustments {
		var effectiveDate time.Time
		if strings.TrimSpace(adjustments[i].EffectiveDate) != "" {
			effectiveDate, err = parseDate(adjustments[i].EffectiveDate)
			if err != nil {
				return nil, fmt.Errorf("invalid adjustment date %s: %w", adjustments[i].EffectiveDate, err)
			}
		} else if strings.TrimSpace(adjustments[i].EffectiveYM) != "" {
			effectiveYM, parseErr := parseYM(adjustments[i].EffectiveYM)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid adjustment ym %s: %w", adjustments[i].EffectiveYM, parseErr)
			}
			effectiveDate = time.Date(effectiveYM.year, time.Month(effectiveYM.month), 1, 0, 0, 0, 0, time.UTC)
		} else {
			return nil, fmt.Errorf("invalid adjustment: effective date is empty")
		}
		rate, parseErr := parseRate(adjustments[i].AnnualRate)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid adjustment rate %s: %w", adjustments[i].AnnualRate, parseErr)
		}
		c.rateTimeline = append(c.rateTimeline, normalizedAdjustment{effectiveDate: effectiveDate, rate: rate})
	}
	sort.Slice(c.rateTimeline, func(i, j int) bool {
		return c.rateTimeline[i].effectiveDate.Before(c.rateTimeline[j].effectiveDate)
	})

	c.prepayments = make([]normalizedPrepayment, 0, len(prepayments))
	for i := range prepayments {
		prepayDate, parseErr := parseDate(prepayments[i].PrepaymentDate)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid prepayment date %s: %w", prepayments[i].PrepaymentDate, parseErr)
		}
		amount, parseErr := parseMoney(prepayments[i].Amount)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid prepayment amount %s: %w", prepayments[i].Amount, parseErr)
		}
		if amount <= 0 {
			continue
		}
		recalcEMI := prepayments[i].Mode == data.PrepaymentModeKeepTermReducePayment
		c.prepayments = append(c.prepayments, normalizedPrepayment{
			date:      prepayDate,
			amount:    amount,
			recalcEMI: recalcEMI,
		})
	}
	sort.Slice(c.prepayments, func(i, j int) bool {
		return c.prepayments[i].date.Before(c.prepayments[j].date)
	})

	currentRate := baseRate
	remaining := principal
	repaymentDay := repaymentDayFromLoan(loan, startDate)
	firstDue := firstDueDate(startDate, repaymentDay)
	firstExtraDays := firstPeriodExtraInterestDays(startDate, repaymentDay)
	prevDue := startDate
	cumPrincipal := 0.0
	cumInterest := 0.0
	currentPayment := annuityPayment(remaining, currentRate, int(loan.TermMonths))
	if currentPayment <= 0 {
		currentPayment = remaining / float64(loan.TermMonths)
	}
	prepayIdx := 0

	for payIdx := 0; payIdx < int(loan.TermMonths); payIdx++ {
		dueMonth := time.Date(firstDue.Year(), firstDue.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, payIdx, 0)
		dueDate := dueDateOfMonth(dueMonth.Year(), dueMonth.Month(), repaymentDay)
		if remaining <= 0.005 {
			remaining = 0
			break
		}
		rateBeforeAdjust := currentRate
		appliedNewRate := false
		appliedDate := time.Time{}
		shouldRecalcPayment := false
		for i := 0; i < len(c.rateTimeline); i++ {
			if !dueDate.Before(c.rateTimeline[i].effectiveDate) {
				if currentRate != c.rateTimeline[i].rate {
					currentRate = c.rateTimeline[i].rate
					appliedNewRate = true
					appliedDate = c.rateTimeline[i].effectiveDate
					shouldRecalcPayment = true
				}
				continue
			}
			break
		}

		prepaymentPrincipal := 0.0
		for prepayIdx < len(c.prepayments) && !c.prepayments[prepayIdx].date.After(dueDate) {
			if c.prepayments[prepayIdx].date.After(prevDue) || c.prepayments[prepayIdx].date.Equal(prevDue) {
				paid := c.prepayments[prepayIdx].amount
				if paid > remaining {
					paid = remaining
				}
				if paid > 0 {
					remaining -= paid
					prepaymentPrincipal += paid
					if c.prepayments[prepayIdx].recalcEMI {
						shouldRecalcPayment = true
					}
				}
			}
			prepayIdx++
		}
		if remaining <= 0.005 {
			remaining = 0
			cumPrincipal += prepaymentPrincipal
			break
		}

		monthsLeft := int(loan.TermMonths) - payIdx
		if shouldRecalcPayment || payIdx == 0 {
			currentPayment = annuityPayment(remaining, currentRate, monthsLeft)
		}
		payment := currentPayment
		regularInterest := remaining * currentRate / 12
		carryoverInterest := 0.0
		if appliedNewRate && rateBeforeAdjust != currentRate {
			if appliedDate.After(prevDue) && !appliedDate.After(dueDate) {
				days := carryoverSpreadDaysByDate(prevDue, appliedDate)
				carryoverInterest = remaining * (rateBeforeAdjust - currentRate) * float64(days) / 360
			}
		}
		dueYM := ymFromDate(dueDate)
		extraInterest := 0.0
		if payIdx == 0 && firstExtraDays > 0 {
			extraInterest = remaining * currentRate * float64(firstExtraDays) / 360
		}
		interest := regularInterest + extraInterest + carryoverInterest
		principalPaid := payment - regularInterest
		if principalPaid <= 0 {
			principalPaid = remaining / float64(monthsLeft)
		}
		if principalPaid > remaining {
			principalPaid = remaining
		}
		remaining -= principalPaid
		if remaining <= 0.005 {
			remaining = 0
		}
		cumPrincipal += principalPaid
		cumPrincipal += prepaymentPrincipal
		cumInterest += interest
		c.records = append(c.records, loanRepaymentRecord{
			Period:              payIdx + 1,
			DueDate:             dueDate,
			YM:                  dueYM.String(),
			MonthlyPrincipal:    round2(principalPaid),
			MonthlyInterest:     round2(interest),
			PrepaymentPrincipal: round2(prepaymentPrincipal),
			RemainingPrincipal:  round2(math.Max(remaining, 0)),
			CumulativePrincipal: round2(cumPrincipal),
			CumulativeInterest:  round2(cumInterest),
		})
		prevDue = dueDate
	}
	return c, nil
}

func (c *loanComputation) snapshotForYM(targetRawYM string) (loanMonthSnapshot, error) {
	snapshot := loanMonthSnapshot{}
	targetYM, err := parseYM(targetRawYM)
	if err != nil {
		return snapshot, fmt.Errorf("invalid target ym: %w", err)
	}
	targetMonthEnd := monthEnd(targetYM)
	if !targetMonthEnd.After(c.loanDate) {
		return snapshot, nil
	}
	snapshot.Include = true
	if c.initialAmount <= 0 {
		return snapshot, nil
	}
	if !c.hasRepayment || !targetMonthEnd.After(c.startDate) {
		snapshot.RemainingPrincipal = c.initialAmount
		return snapshot, nil
	}
	remaining := c.initialAmount
	for i := range c.records {
		if c.records[i].DueDate.After(targetMonthEnd) {
			break
		}
		remaining = c.records[i].RemainingPrincipal
		if c.records[i].YM == targetYM.String() {
			snapshot.MonthlyPrincipal = c.records[i].MonthlyPrincipal
			snapshot.MonthlyInterest = c.records[i].MonthlyInterest
			snapshot.PrepaymentPrincipal = c.records[i].PrepaymentPrincipal
		}
	}
	snapshot.RemainingPrincipal = round2(remaining)
	return snapshot, nil
}

func (c *loanComputation) rateAtDate(targetDate time.Time) (float64, error) {
	baseRate, err := parseRate(c.loan.AnnualRate)
	if err != nil {
		return 0, err
	}
	rate := baseRate
	for i := range c.rateTimeline {
		if !targetDate.Before(c.rateTimeline[i].effectiveDate) {
			rate = c.rateTimeline[i].rate
		}
	}
	return rate, nil
}

type loanSummaryData struct {
	RemainingPrincipal  float64
	RepaidMonths        int
	CumulativePrincipal float64
	CumulativeInterest  float64
	RemainingInterest   float64
	NextDueDate         string
	AnnualRate          float64
}

func (c *loanComputation) totalPlannedInterest() float64 {
	if len(c.records) == 0 {
		return 0
	}
	return c.records[len(c.records)-1].CumulativeInterest
}

func (c *loanComputation) summaryAt(now time.Time) (loanSummaryData, error) {
	s := loanSummaryData{RemainingPrincipal: c.initialAmount}
	rate, err := c.rateAtDate(now)
	if err != nil {
		return s, err
	}
	s.AnnualRate = rate
	if len(c.records) == 0 {
		return s, nil
	}
	for i := range c.records {
		if c.records[i].DueDate.After(now) {
			s.NextDueDate = formatDate(c.records[i].DueDate)
			break
		}
		s.RepaidMonths = c.records[i].Period
		s.CumulativePrincipal = c.records[i].CumulativePrincipal
		s.CumulativeInterest = c.records[i].CumulativeInterest
		s.RemainingPrincipal = c.records[i].RemainingPrincipal
	}
	if s.RepaidMonths == 0 {
		s.RemainingPrincipal = c.initialAmount
	}
	totalInterest := c.totalPlannedInterest()
	remainingInterest := totalInterest - s.CumulativeInterest
	if remainingInterest < 0 {
		remainingInterest = 0
	}
	s.RemainingInterest = round2(remainingInterest)
	return s, nil
}

func (c *loanComputation) futurePlans(now time.Time, months int) []loanRepaymentRecord {
	if months <= 0 {
		months = 24
	}
	plans := make([]loanRepaymentRecord, 0, months)
	for i := range c.records {
		if !c.records[i].DueDate.After(now) {
			continue
		}
		plans = append(plans, c.records[i])
		if len(plans) >= months {
			break
		}
	}
	return plans
}
