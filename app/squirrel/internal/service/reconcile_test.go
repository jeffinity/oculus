package service

import (
	"testing"

	"github.com/jeffinity/oculus/app/squirrel/internal/data"
)

func TestBuildWechatMatchMapAcceptsMainOrderID(t *testing.T) {
	columns := []string{"入账时间", "支付流水号", "主订单ID", "入帐类型"}
	rows := []data.SquirrelSectionRowData{
		{
			RowNo:  3,
			Values: []string{"2026-04-25 13:13:45", "849002276510518", "3297493634735066590", "交易收款"},
		},
	}
	out := make(map[string][]reconcileMatchItem)

	if err := buildWechatMatchMap(columns, rows, out); err != nil {
		t.Fatalf("buildWechatMatchMap() error = %v", err)
	}
	matches := out["3297493634735066590"]
	if len(matches) != 1 {
		t.Fatalf("matched rows = %d, want 1", len(matches))
	}
	if matches[0].RowNo != 3 {
		t.Fatalf("matched row_no = %d, want 3", matches[0].RowNo)
	}
}
