package scaffold

import "github.com/SuperQuail/QuailEgg/internal/i18n"

// changesReadme returns the guide placed inside the change record directory.
// The text is localized prose, so it stays in the message catalog instead of
// becoming an asset file.
func changesReadme() string {
	return i18n.T("scaffold.changes_readme")
}
