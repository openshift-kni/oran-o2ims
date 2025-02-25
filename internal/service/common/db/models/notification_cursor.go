package models

// NotificationCursor db to track the high watermark
type NotificationCursor struct {
	ID          int `db:"id"`
	LastEventID int `db:"last_event_id"`
}

// TableName returns the name of the table in the database
func (r NotificationCursor) TableName() string {
	return "notification_cursor"
}

// PrimaryKey returns the primary key of the table
func (r NotificationCursor) PrimaryKey() string {
	return "id"
}

// OnConflict returns the column or constraint to be used in the UPSERT operation
func (r NotificationCursor) OnConflict() string {
	return ""
}
