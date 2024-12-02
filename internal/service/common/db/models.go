package db

type Model interface {
	PrimaryKey() string
	TableName() string
}
