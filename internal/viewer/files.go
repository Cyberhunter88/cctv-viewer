package viewer

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

// os.Root pins resolution to the mounted directory, including symlink targets.
func validRelative(p string) bool {
	return p != "" && p != "." && fs.ValidPath(p) && !strings.Contains(p, "\\")
}
func openSource(root *os.Root, p string) (*os.File, error) {
	if !validRelative(p) {
		return nil, errors.New("ungültiger Aufnahmepfad")
	}
	f, e := root.Open(p)
	if e != nil {
		return nil, e
	}
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("keine reguläre Datei")
	}
	return f, nil
}

var tokens = map[string]string{"YYYY": `(?P<year>\d{4})`, "MM": `(?P<month>\d{2})`, "DD": `(?P<day>\d{2})`, "hh": `(?P<hour>\d{2})`, "mm": `(?P<minute>\d{2})`, "ss": `(?P<second>\d{2})`}

func patternRE(pattern string) (*regexp.Regexp, error) {
	if !validRelative(pattern) || len(pattern) > 300 {
		return nil, errors.New("ungültiges Dateimuster")
	}
	s := regexp.QuoteMeta(pattern)
	for k, v := range tokens {
		if strings.Count(pattern, "{"+k+"}") != 1 {
			return nil, fmt.Errorf("Muster benötigt genau einmal {%s}", k)
		}
		s = strings.ReplaceAll(s, regexp.QuoteMeta("{"+k+"}"), v)
	}
	return regexp.Compile("^" + s + "$")
}
func ParseTimestamp(pattern, name, zone string, offset float64) (time.Time, error) {
	re, e := patternRE(pattern)
	if e != nil {
		return time.Time{}, e
	}
	match := re.FindStringSubmatch(name)
	if match == nil {
		return time.Time{}, errors.New("Dateiname passt nicht zum Muster")
	}
	parts := map[string]string{}
	for i, key := range re.SubexpNames() {
		if i > 0 {
			parts[key] = match[i]
		}
	}
	local := parts["year"] + "-" + parts["month"] + "-" + parts["day"] + " " + parts["hour"] + ":" + parts["minute"] + ":" + parts["second"]
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return time.Time{}, e
	}
	t, e := time.ParseInLocation("2006-01-02 15:04:05", local, loc)
	if e != nil {
		return t, e
	}
	if t.Format("2006-01-02 15:04:05") != local {
		return time.Time{}, errors.New("Uhrzeit existiert wegen Zeitumstellung nicht")
	}
	for _, delta := range []time.Duration{-time.Hour, time.Hour} {
		if t.Add(delta).In(loc).Format("2006-01-02 15:04:05") == local {
			return time.Time{}, errors.New("Uhrzeit ist wegen Winterzeitumstellung mehrdeutig")
		}
	}
	return t.Add(time.Duration(offset * float64(time.Second))).UTC(), nil
}
func validateCamera(c Camera, root *os.Root) error {
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 || !validRelative(c.Folder) || c.Offset < -86400 || c.Offset > 86400 || math.IsNaN(c.Offset) || math.IsInf(c.Offset, 0) || math.Abs(c.Offset*10-math.Round(c.Offset*10)) > 0.000001 {
		return errors.New("Name, Ordner oder Zeitversatz ungültig")
	}
	if c.IndexingMode != "" && c.IndexingMode != "partitioned" && c.IndexingMode != "full" {
		return errors.New("Indexierungsmodus ungültig")
	}
	switch cameraFormat(c) {
	case "automatic":
		if c.IndexingMode != "full" {
			if e := validateDirectoryPattern(c.DirectoryPattern); e != nil {
				return e
			}
		}
	case "reolink":
		// The camera folder identifies the source; legacy prefixes are ignored.
	case "custom":
		if _, e := patternRE(c.Pattern); e != nil {
			return e
		}
	default:
		return errors.New("Unbekanntes Aufnahmeformat")
	}
	if _, e := time.LoadLocation(c.Timezone); e != nil {
		return errors.New("unbekannte Zeitzone")
	}
	f, e := root.Open(c.Folder)
	if e != nil {
		return errors.New("Kameraordner nicht erreichbar")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.IsDir() {
		return errors.New("Kameraquelle muss ein Ordner sein")
	}
	return nil
}
func partition(c Camera, day time.Time) (string, error) {
	if c.IndexingMode == "full" {
		return c.Folder, nil
	}
	if cameraFormat(c) == "automatic" {
		pattern := c.DirectoryPattern
		if pattern == "" {
			pattern = "{YYYY}/{MM}/{DD}"
		}
		p := path.Join(c.Folder, strings.NewReplacer("{YYYY}", day.Format("2006"), "{YY}", day.Format("06"), "{MM}", day.Format("01"), "{DD}", day.Format("02")).Replace(pattern))
		if !validRelative(p) {
			return "", errors.New("Tagespartition ungültig")
		}
		return p, nil
	}
	if cameraFormat(c) == "reolink" {
		return path.Join(c.Folder, day.Format("2006/01/02")), nil
	}
	// Only use the prefix before the first filename component containing a clock token.
	components := strings.Split(c.Pattern, "/")
	var dirs []string
	for _, component := range components[:len(components)-1] {
		if strings.Contains(component, "{hh}") || strings.Contains(component, "{mm}") || strings.Contains(component, "{ss}") {
			break
		}
		component = strings.NewReplacer("{YYYY}", day.Format("2006"), "{MM}", day.Format("01"), "{DD}", day.Format("02")).Replace(component)
		dirs = append(dirs, component)
	}
	p := path.Join(append([]string{c.Folder}, dirs...)...)
	if !validRelative(p) {
		return "", errors.New("ungültige Tagespartition")
	}
	return p, nil
}

func cameraFormat(c Camera) string {
	if c.Format == "" {
		return "custom"
	}
	return c.Format
}
func cameraTimestamp(c Camera, name string) (time.Time, error) {
	if cameraFormat(c) == "automatic" {
		for _, re := range timestampPatterns {
			if m := re.FindStringSubmatch(path.Base(name)); m != nil {
				return ParseTimestamp("{YYYY}{MM}{DD}{hh}{mm}{ss}", strings.Join(m[1:], ""), c.Timezone, c.Offset)
			}
		}
		return time.Time{}, errors.New("Kein Zeitstempel im Dateinamen")
	}
	if cameraFormat(c) != "reolink" {
		pattern := c.Pattern
		if ext := strings.ToLower(path.Ext(name)); ext == ".jpg" || ext == ".jpeg" {
			if contains([]string{".mp4", ".mov", ".mkv", ".avi", ".ts", ".h264", ".h265"}, strings.ToLower(path.Ext(pattern))) {
				pattern = strings.TrimSuffix(pattern, path.Ext(pattern)) + path.Ext(name)
			}
		}
		return ParseTimestamp(pattern, name, c.Timezone, c.Offset)
	}
	re := regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2})/[^/]+_[0-9]+_([0-9]{14})\.(?i:mp4|jpg|jpeg)$`)
	m := re.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, errors.New("Dateiname passt nicht zum Reolink-Format")
	}
	if m[1]+m[2]+m[3] != m[4][:8] {
		return time.Time{}, errors.New("Ordnerdatum und Dateidatum stimmen nicht überein")
	}
	return ParseTimestamp("{YYYY}{MM}{DD}{hh}{mm}{ss}", m[4], c.Timezone, c.Offset)
}
func supportedMedia(name string) bool {
	return contains([]string{".mp4", ".mov", ".mkv", ".avi", ".ts", ".h264", ".h265", ".dav"}, strings.ToLower(path.Ext(name)))
}

var timestampPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(\d{4})(\d{2})(\d{2})_?(\d{2})(\d{2})(\d{2})`),
	regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})_?(\d{2})-?(\d{2})-?(\d{2})`),
}

func validateDirectoryPattern(p string) error {
	if p == "" {
		return nil
	}
	if !validRelative(p) || len(p) > 300 {
		return errors.New("Verzeichnismuster ungültig")
	}
	stripped := strings.NewReplacer("{YYYY}", "", "{YY}", "", "{MM}", "", "{DD}", "").Replace(p)
	if strings.ContainsAny(stripped, "{}") || (strings.Count(p, "{YYYY}")+strings.Count(p, "{YY}") != 1) || strings.Count(p, "{MM}") != 1 || strings.Count(p, "{DD}") != 1 {
		return errors.New("Verzeichnismuster benötigt Jahr, Monat und Tag")
	}
	return nil
}
