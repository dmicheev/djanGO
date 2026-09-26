package gd

type Settings struct {
	Addr         string
	TemplatesDir string
	StaticDir    string
	SecretKey    string
	Debug        bool
}

func DefaultSettings() Settings {
	return Settings{
		Addr:         ":8000",
		TemplatesDir: "templates",
		SecretKey:    "change-me",
		Debug:        true,
	}
}
