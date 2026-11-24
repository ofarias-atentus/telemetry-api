package api

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const isoCodes = `
AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ
BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ
CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ
DE DJ DK DM DO DZ
EC EE EG EH ER ES ET
FI FJ FK FM FO FR
GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY
HK HM HN HR HT HU
ID IE IL IM IN IO IQ IR IS IT
JE JM JO JP
KE KG KH KI KM KN KP KR KW KY KZ
LA LB LC LI LK LR LS LT LU LV LY
MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ
NA NC NE NF NG NI NL NO NP NR NU NZ
OM
PA PE PF PG PH PK PL PM PN PR PS PT PW PY
QA
RE RO RS RU RW
SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ
TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ
UA UG UM US UY UZ
VA VC VE VG VI VN VU
WF WS
YE YT
ZA ZM ZW
`

const wantCountries = 249

var (
	countryRowRe = regexp.MustCompile(
		`(?m)^\s*\('([A-Z]{2})', '((?:[^']|'')+)', ARRAY\[([^\]]*)\]::text\[\]\),?[ \t]*$`,
	)
	sqlStringRe = regexp.MustCompile(`'((?:[^']|'')*)'`)
)

type countryRow struct {
	code    string
	name    string
	aliases []string
}

func unescapeSQL(s string) string {
	return strings.ReplaceAll(s, "''", "'")
}

func readSchemaFile(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

func countriesInsert(t *testing.T, sql string) string {
	t.Helper()

	start := strings.Index(sql, "INSERT INTO countries")
	if start < 0 {
		t.Fatal("no INSERT INTO countries block")
	}
	end := strings.Index(sql[start:], ";")
	if end < 0 {
		t.Fatal("INSERT INTO countries block has no terminating semicolon")
	}
	return sql[start : start+end+1]
}

func parseCountries(block string) []countryRow {
	var rows []countryRow
	for _, m := range countryRowRe.FindAllStringSubmatch(block, -1) {
		row := countryRow{code: m[1], name: unescapeSQL(m[2])}
		for _, a := range sqlStringRe.FindAllStringSubmatch(m[3], -1) {
			row.aliases = append(row.aliases, unescapeSQL(a[1]))
		}
		rows = append(rows, row)
	}
	return rows
}

func normalizeSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestCountriesSeed(t *testing.T) {
	t.Parallel()

	block := countriesInsert(t, readSchemaFile(t, "../../migrations/003_countries.sql"))
	rows := parseCountries(block)

	t.Run("every line of the block is a parsed row", func(t *testing.T) {
		t.Parallel()

		lines := 0
		for _, l := range strings.Split(block, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "('") {
				lines++
			}
		}
		if lines != len(rows) {
			t.Errorf("row lines = %d, parsed rows = %d", lines, len(rows))
		}
	})

	t.Run("has 249 rows", func(t *testing.T) {
		t.Parallel()

		if len(rows) != wantCountries {
			t.Errorf("rows = %d, want %d", len(rows), wantCountries)
		}
	})

	t.Run("codes are unique", func(t *testing.T) {
		t.Parallel()

		seen := map[string]bool{}
		for _, r := range rows {
			if seen[r.code] {
				t.Errorf("duplicate code %s", r.code)
			}
			seen[r.code] = true
		}
	})

	t.Run("codes equal the ISO 3166-1 alpha-2 list", func(t *testing.T) {
		t.Parallel()

		want := map[string]bool{}
		for _, c := range strings.Fields(isoCodes) {
			want[c] = true
		}
		if len(want) != wantCountries {
			t.Fatalf("test list has %d codes, want %d", len(want), wantCountries)
		}

		got := map[string]bool{}
		for _, r := range rows {
			got[r.code] = true
			if !want[r.code] {
				t.Errorf("code %s is not in the ISO list", r.code)
			}
		}
		for c := range want {
			if !got[c] {
				t.Errorf("ISO code %s is missing from the seed", c)
			}
		}
	})

	t.Run("names are unique ignoring case", func(t *testing.T) {
		t.Parallel()

		seen := map[string]string{}
		for _, r := range rows {
			key := strings.ToLower(r.name)
			if other, ok := seen[key]; ok {
				t.Errorf("name of %s repeats name of %s", r.code, other)
			}
			seen[key] = r.code
		}
	})

	t.Run("aliases are lowercase and trimmed", func(t *testing.T) {
		t.Parallel()

		for _, r := range rows {
			for _, a := range r.aliases {
				if a != strings.ToLower(a) || a != strings.TrimSpace(a) || a == "" {
					t.Errorf("%s: alias %q is not lowercase and trimmed", r.code, a)
				}
			}
		}
	})

	t.Run("no value resolves to more than one country", func(t *testing.T) {
		t.Parallel()

		// Un alias igual al nombre en minúsculas del mismo país es válido: lower() de Postgres no pliega tildes en locale C.
		owner := map[string]string{}
		claim := func(value, code string) {
			if other, ok := owner[value]; ok && other != code {
				t.Errorf("%q resolves to both %s and %s", value, other, code)
			}
			owner[value] = code
		}
		for _, r := range rows {
			claim(strings.ToLower(r.code), r.code)
		}
		for _, r := range rows {
			claim(strings.ToLower(r.name), r.code)
		}
		for _, r := range rows {
			for _, a := range r.aliases {
				claim(a, r.code)
			}
		}
	})

	t.Run("spot checks", func(t *testing.T) {
		t.Parallel()

		byCode := map[string]countryRow{}
		for _, r := range rows {
			byCode[r.code] = r
		}

		names := map[string]string{
			"CL": "Chile",
			"PE": "Perú",
			"MX": "México",
			"US": "Estados Unidos",
			"GB": "Reino Unido",
		}
		for code, name := range names {
			if got := byCode[code].name; got != name {
				t.Errorf("%s name = %q, want %q", code, got, name)
			}
		}

		hasAlias := false
		for _, a := range byCode["CI"].aliases {
			if a == "cote d'ivoire" {
				hasAlias = true
			}
		}
		if !hasAlias {
			t.Errorf("CI aliases = %q, want to include %q", byCode["CI"].aliases, "cote d'ivoire")
		}
	})
}

func TestCountriesSeedMatchesDatabaseSQL(t *testing.T) {
	t.Parallel()

	migration := countriesInsert(t, readSchemaFile(t, "../../migrations/003_countries.sql"))
	fresh := countriesInsert(t, readSchemaFile(t, "../../database.sql"))

	if migration != fresh {
		t.Error("INSERT INTO countries differs between migrations/003_countries.sql and database.sql")
	}
}

func TestCountryMatchingPredicateInSync(t *testing.T) {
	t.Parallel()

	predicate := func(x string) string {
		return "c.code = upper(btrim(" + x + ")) " +
			"OR lower(c.name) = lower(btrim(" + x + ")) " +
			"OR lower(btrim(" + x + ")) = ANY(c.aliases)"
	}

	t.Run("upsert query", func(t *testing.T) {
		t.Parallel()

		query := strings.ReplaceAll(normalizeSQL(upsertDeviceQuery), "$6::text", "$6")
		if got := strings.Count(query, predicate("$6")); got != 1 {
			t.Errorf("upsertDeviceQuery has the predicate %d times, want 1", got)
		}
	})

	t.Run("migration 003", func(t *testing.T) {
		t.Parallel()

		migration := normalizeSQL(readSchemaFile(t, "../../migrations/003_countries.sql"))
		if got := strings.Count(migration, predicate("d.country")); got != 2 {
			t.Errorf("migration 003 has the predicate %d times (DO block and UPDATE), want 2", got)
		}
	})
}
