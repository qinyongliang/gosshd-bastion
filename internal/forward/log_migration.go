package forward

import "database/sql"

func upgradeLogTotals(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(totals)")
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for rows.Next() {
		var index, notnull, primary int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&index, &name, &kind, &notnull, &defaultValue, &primary); err != nil {
			rows.Close()
			return err
		}
		present[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range []string{"direct_up", "direct_down"} {
		if !present[name] {
			if _, err := db.Exec("ALTER TABLE totals ADD COLUMN " + name + " INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
		}
	}
	return nil
}
