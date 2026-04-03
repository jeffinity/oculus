package service

import (
	"testing"
	"time"

	"github.com/jeffinity/oculus/app/bookkeeping/internal/data"
)

func TestLoanSummaryAtIncludesPrepaymentBeforeDueDate(t *testing.T) {
	loan := data.Loan{
		LoanID:           "loan-1",
		LoanName:         "商业房贷",
		LoanType:         data.LoanTypeMortgage,
		LoanDate:         "2023-11-23",
		InitialPrincipal: "1980000.00",
		AnnualRate:       "0.035",
		TermMonths:       360,
		StartDate:        "2024-07-03",
		RepaymentDay:     20,
	}
	adjustments := []data.LoanRateAdjustment{
		{LoanID: "loan-1", EffectiveDate: "2025-01-03", AnnualRate: "0.0315"},
		{LoanID: "loan-1", EffectiveDate: "2025-07-03", AnnualRate: "0.0305"},
	}
	prepayments := []data.LoanPrepayment{
		{
			LoanID:         "loan-1",
			PrepaymentDate: "2026-04-03",
			Amount:         "150000.00",
			Mode:           data.PrepaymentModeKeepPaymentShortenTerm,
		},
	}

	comp, err := buildLoanComputation(loan, adjustments, prepayments)
	if err != nil {
		t.Fatalf("buildLoanComputation() error = %v", err)
	}
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	summary, err := comp.summaryAt(now)
	if err != nil {
		t.Fatalf("summaryAt() error = %v", err)
	}

	if summary.RepaidMonths != 20 {
		t.Fatalf("RepaidMonths = %d, want 20", summary.RepaidMonths)
	}
	if summary.CumulativeInterest != 107567.62 {
		t.Fatalf("CumulativeInterest = %.2f, want 107567.62", summary.CumulativeInterest)
	}
	if summary.CumulativePrincipal != 217465.88 {
		t.Fatalf("CumulativePrincipal = %.2f, want 217465.88", summary.CumulativePrincipal)
	}
	if summary.RemainingPrincipal != 1762534.12 {
		t.Fatalf("RemainingPrincipal = %.2f, want 1762534.12", summary.RemainingPrincipal)
	}
	if summary.NextDueDate != "2026-04-20" {
		t.Fatalf("NextDueDate = %s, want 2026-04-20", summary.NextDueDate)
	}
	if summary.EstimatedPayoffDate != "2051-03-20" {
		t.Fatalf("EstimatedPayoffDate = %s, want 2051-03-20", summary.EstimatedPayoffDate)
	}
	if summary.ShortenedMonths != 40 {
		t.Fatalf("ShortenedMonths = %d, want 40", summary.ShortenedMonths)
	}
}
