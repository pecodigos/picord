package profile

import (
	"strings"
)

// RenderActivity replaces template variables in activity strings with
// values from the detected process.
//
// Supported variables:
//
//	{process_name}  - the detected process name
//	{window_title}  - the detected window title
//	{title}         - canonical game/app title (catalog or profile)
//	{source}        - catalog source (e.g., steam, lutris, desktop)
//	{steam_app_id}  - Steam AppID if detected
func RenderActivity(act Activity, proc DetectedProcess) Activity {
	replacer := strings.NewReplacer(
		"{process_name}", proc.Name,
		"{window_title}", proc.WindowTitle,
		"{steam_app_id}", proc.SteamAppID,
	)

	return Activity{
		Details:    replacer.Replace(act.Details),
		State:      replacer.Replace(act.State),
		LargeImage: act.LargeImage,
		LargeText:  replacer.Replace(act.LargeText),
		SmallImage: act.SmallImage,
		SmallText:  replacer.Replace(act.SmallText),
		Buttons:    act.Buttons,
		PartyID:    act.PartyID,
	}
}
