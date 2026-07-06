package data

import (
	"time"

	"github.com/jeffinity/singularity/pgx"
	"gorm.io/datatypes"
)

type MHBook struct {
	pgx.BaseModel

	Title     string         `gorm:"column:title;type:text;not null"`
	URL       string         `gorm:"column:url;type:text;not null"`
	Desc      string         `gorm:"column:desc;type:text;not null;default:''"`
	OrgID     int64          `gorm:"column:org_id;type:bigint;not null;unique;uniqueIndex:uk_mh_books_org_id"`
	Cover     datatypes.JSON `gorm:"column:cover;type:jsonb;not null;default:'{}'::jsonb"`
	CreatedAt time.Time      `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;index:idx_mh_books_created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
}

func (*MHBook) TableName() string { return "books" }

type MHLatest struct {
	ID        int64     `gorm:"column:id;type:bigserial;primaryKey;autoIncrement"`
	Prefix    string    `gorm:"column:prefix;type:text;not null"`
	CID       int64     `gorm:"column:cid;type:bigint;not null"`
	CName     string    `gorm:"column:cname;type:text;not null"`
	OrgID     int64     `gorm:"column:org_id;type:bigint;not null;index:idx_mh_latest_org_id"`
	Title     string    `gorm:"column:title;type:text;not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;index:idx_mh_latest_created_at"`
}

func (*MHLatest) TableName() string { return "latest" }

type MHHistory struct {
	OrgID     int64     `gorm:"column:org_id;type:bigint;primaryKey"`
	CID       int64     `gorm:"column:cid;type:bigint;not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
}

func (*MHHistory) TableName() string { return "history" }

type MHProofHistory struct {
	OrgID     int64     `gorm:"column:org_id;type:bigint;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
}

func (*MHProofHistory) TableName() string { return "proof" }

type MHSyncState struct {
	ID        string    `gorm:"column:id;type:text;primaryKey"`
	CID       int64     `gorm:"column:cid;type:bigint;not null"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
}

func (*MHSyncState) TableName() string { return "sync_state" }

type MHStar struct {
	OrgID     int64     `gorm:"column:org_id;type:bigint;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
}

func (*MHStar) TableName() string { return "stars" }
