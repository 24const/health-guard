package migrations

import "embed"

//go:embed postgres
var postgresMigration embed.FS

func GetPostgresMigrations() embed.FS {
	return postgresMigration
}
