package http

import "testing"

func TestBackupNames(t *testing.T) {
	for _, name := range []string{
		"backup_20261022_093000.sql", "backup_20261022_093000_0042.sql", "pre_restore_20261022_093000_0001.sql",
		"pre_update_v1.0.0_to_v1.1.0_20261022_093000.sql", "pre_update_v1.1.0_to_v1.2.0-beta.1_20261022_093000.sql",
	} {
		if !backupFilenameRe.MatchString(name) {
			t.Errorf("%s is not listed", name)
		}
	}
	for _, name := range []string{
		"pre_update_v1.0.0_to_v1.1.0.sql", "pre_update_../../x_to_v1_20261022_093000.sql",
		"pre_update_v1/x_to_v2_20261022_093000.sql", "backup_20261022_093000.sql.gz",
	} {
		if backupFilenameRe.MatchString(name) {
			t.Errorf("%s is listed", name)
		}
	}
}
