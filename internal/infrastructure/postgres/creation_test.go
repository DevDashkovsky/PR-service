package postgres

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConnectionURLEscapesCredentialsAndDatabase(t *testing.T) {
	values := map[string]string{"DB_USER": "user@name", "DB_PASSWORD": "p@ss:/?#% word", "DB_HOST": "::1", "DB_PORT": "5432", "DB_NAME": "database/with?#% name"}
	for key, value := range values {
		t.Setenv(key, value)
	}
	path, err := (&Postgres{}).createConnectPath()
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.User != values["DB_USER"] || config.ConnConfig.Password != values["DB_PASSWORD"] || config.ConnConfig.Database != values["DB_NAME"] || config.ConnConfig.Host != values["DB_HOST"] {
		t.Fatal("connection URL changed a parameter")
	}
}
