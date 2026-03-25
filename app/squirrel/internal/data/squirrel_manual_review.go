package data

import "time"

const manualReviewTableName = "squirrel_manual_reviews"

const (
	ManualReviewTypeManual = "MANUAL"
	ManualReviewTypeAuto   = "AUTO"
)

type SquirrelManualReview struct {
	ID           uint64    `gorm:"column:id;primaryKey;autoIncrement;comment:自增ID"`
	TaskID       string    `gorm:"column:task_id;type:varchar(26);not null;uniqueIndex:uk_squirrel_manual_task_file_row,priority:1;index:idx_squirrel_manual_task,priority:1;comment:任务ID"`
	Filename     string    `gorm:"column:filename;type:varchar(255);not null;default:'';uniqueIndex:uk_squirrel_manual_task_file_row,priority:2;comment:文件名"`
	RowNo        int32     `gorm:"column:row_no;type:int;not null;uniqueIndex:uk_squirrel_manual_task_file_row,priority:3;comment:行号"`
	RefundQty    string    `gorm:"column:refund_qty;type:varchar(64);not null;default:'';comment:退款数量"`
	RefundAmount string    `gorm:"column:refund_amount;type:varchar(64);not null;default:'';comment:退款金额"`
	SettleQty    string    `gorm:"column:settle_qty;type:varchar(64);not null;default:'';comment:结算数量"`
	SettleAmount string    `gorm:"column:settle_amount;type:varchar(64);not null;default:'';comment:结算金额"`
	Remark       string    `gorm:"column:remark;type:varchar(500);not null;default:'';comment:备注"`
	ReviewType   string    `gorm:"column:review_type;type:varchar(16);not null;default:'MANUAL';comment:核查类型(MANUAL/AUTO)"`
	Ignored      bool      `gorm:"column:ignored;type:boolean;not null;default:false;comment:是否忽略"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamptz;not null;comment:创建时间"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

func (*SquirrelManualReview) TableName() string { return manualReviewTableName }
