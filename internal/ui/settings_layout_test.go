package ui

import (
	"testing"

	"github.com/xiongnemo/saki/internal/i18n"
	"github.com/xiongnemo/saki/internal/models"
)

func TestSettingLabelWidthUsesTerminalCellWidth(t *testing.T) {
	previous := i18n.Current()
	defer i18n.SetCurrent(string(previous))

	i18n.SetCurrent(models.LanguageChinese)
	if got := textCellWidth("中文"); got != 4 {
		t.Fatalf("Chinese cell width = %d, want 4", got)
	}
	rows := []settingRow{{label: "Language"}}
	if got := settingLabelWidth(rows, 80); got != 6 {
		t.Fatalf("Chinese label column width = %d, want 6", got)
	}
}

func TestLanguageOptionRecognizesCanonicalChineseValue(t *testing.T) {
	if got := languageOption(models.LanguageChinese); got != 2 {
		t.Fatalf("language option for %q = %d, want 2", models.LanguageChinese, got)
	}
}
