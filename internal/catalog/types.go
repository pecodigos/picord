package catalog

import "time"

type EntryKind string

const (
	EntryKindGame        EntryKind = "game"
	EntryKindApplication EntryKind = "application"
)

type Entry struct {
	ID              string
	Source          string
	SourceID        string
	Kind            EntryKind
	Title           string
	NormalizedTitle string
	ReleaseYear     int
	ImageURL        string
	ImageKind       string
	DiscordAssetKey string
	UpdatedAt       time.Time
}

type AliasKind string

const (
	AliasTitle       AliasKind = "title"
	AliasExecutable  AliasKind = "executable"
	AliasWindowTitle AliasKind = "window_title"
	AliasSteamAppID  AliasKind = "steam_app_id"
	AliasLutrisSlug  AliasKind = "lutris_slug"
	AliasDesktopID   AliasKind = "desktop_id"
)

type Alias struct {
	EntryID    string
	Kind       AliasKind
	Value      string
	Normalized string
	Confidence int
}

type Image struct {
	EntryID   string
	URL       string
	CachePath string
	SHA256    string
	Width     int
	Height    int
	MIME      string
	Status    string
	FetchedAt time.Time
}

type MatchResult struct {
	Entry      Entry
	Confidence int
	Reason     string
}
