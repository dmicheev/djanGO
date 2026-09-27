package gd

type Database struct {
	Driver string
	DSN    string
}

type Settings struct {
	Addr         string
	TemplatesDir string
	StaticDir    string
	SecretKey    string
	Debug        bool
	Database     Database
}

func DefaultSettings() Settings {
	return Settings{
		Addr:         ":8000",
		TemplatesDir: "templates",
		SecretKey:    "change-me",
		Debug:        true,
		Database:     Database{Driver: "sqlite", DSN: "app.db"},
	}
}
