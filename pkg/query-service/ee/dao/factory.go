package dao

import (
	"fmt"
	"go.signoz.io/query-service/ee/dao/sqlite"
)

func InitDao(engine, path string) (ModelDao, error) {

	switch engine {
	case "sqlite":
		return sqlite.InitDB(path)
	default:
		return nil, fmt.Errorf("qsdb type: %s is not supported in query service", engine)
	}
	return nil, fmt.Errorf("unexpected error while initializing qsdb")
}