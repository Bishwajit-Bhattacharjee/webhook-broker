// Code generated by Wire. DO NOT EDIT.

//go:generate wire
//+build !wireinject

package storage

import (
	"github.com/imyousuf/webhook-broker/config"
)

import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/mattn/go-sqlite3"
)

// Injectors from wire.go:

func GetNewDataAccessor(dbConfig config.RelationalDatabaseConfig, migrationConf *MigrationConfig, seedDataConfig config.SeedDataConfig) (DataAccessor, error) {
	sqlDB, err := GetConnectionPool(dbConfig, migrationConf, seedDataConfig)
	if err != nil {
		return nil, err
	}
	appRepository := NewAppRepository(sqlDB)
	producerRepository := NewProducerRepository(sqlDB)
	dataAccessor := NewDataAccessor(sqlDB, appRepository, producerRepository)
	return dataAccessor, nil
}
