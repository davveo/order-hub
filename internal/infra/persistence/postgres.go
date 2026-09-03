package persistence

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func postgresDialector(dsn string) gorm.Dialector {
	return postgres.Open(dsn)
}
