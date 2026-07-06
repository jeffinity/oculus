package data

import "time"

const manualRemarkOptionTableName = "squirrel_manual_remark_options"

type SquirrelManualRemarkOption struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement;comment:自增ID"`
	TaskID    string    `gorm:"column:task_id;type:varchar(26);not null;uniqueIndex:uk_squirrel_manual_remark_task_content,priority:1;index:idx_squirrel_manual_remark_task_count,priority:1;comment:任务ID"`
	Content   string    `gorm:"column:content;type:varchar(500);not null;default:'';uniqueIndex:uk_squirrel_manual_remark_task_content,priority:2;comment:备注内容"`
	UsedCount uint32    `gorm:"column:used_count;type:int;not null;default:0;index:idx_squirrel_manual_remark_task_count,priority:2,sort:desc;comment:使用次数"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;comment:创建时间"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null;comment:更新时间"`
}

func (*SquirrelManualRemarkOption) TableName() string { return manualRemarkOptionTableName }
