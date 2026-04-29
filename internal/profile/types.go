package profile

type MatchType string

const (
	MatchProcessName MatchType = "process_name"
	MatchWindowTitle MatchType = "window_title"
	MatchRegex       MatchType = "regex"
)

type Profile struct {
	Name     string    `yaml:"name" json:"name"`
	AppID    string    `yaml:"app_id,omitempty" json:"app_id,omitempty"`
	Match    MatchRule `yaml:"match" json:"match"`
	Activity Activity  `yaml:"activity" json:"activity"`
	Priority int       `yaml:"priority" json:"priority"`
	Enabled  bool      `yaml:"enabled" json:"enabled"`

	isDefault bool `yaml:"-" json:"-"`
}

func (p *Profile) IsDefault() bool   { return p.isDefault }
func (p *Profile) SetDefault(v bool) { p.isDefault = v }

type MatchRule struct {
	Type  MatchType `yaml:"type" json:"type"`
	Value string    `yaml:"value" json:"value"`
}

type Activity struct {
	Details    string   `yaml:"details,omitempty" json:"details,omitempty"`
	State      string   `yaml:"state,omitempty" json:"state,omitempty"`
	LargeImage string   `yaml:"large_image,omitempty" json:"large_image,omitempty"`
	LargeText  string   `yaml:"large_text,omitempty" json:"large_text,omitempty"`
	SmallImage string   `yaml:"small_image,omitempty" json:"small_image,omitempty"`
	SmallText  string   `yaml:"small_text,omitempty" json:"small_text,omitempty"`
	Buttons    []Button `yaml:"buttons,omitempty" json:"buttons,omitempty"`
	PartyID    string   `yaml:"party_id,omitempty" json:"party_id,omitempty"`
}

type Button struct {
	Label string `yaml:"label" json:"label"`
	URL   string `yaml:"url" json:"url"`
}
