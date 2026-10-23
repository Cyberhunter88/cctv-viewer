package viewer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const secretPrefix = "enc:v1:"

type secretColumn struct{ table, id, column string }

var secretColumns = []secretColumn{
	{"camera_live", "camera_id", "payload"}, {"live_share", "id", "payload"}, {"settings", "id", "payload"},
	{"sessions", "hash", "csrf"}, {"oidc_flows", "hash", "nonce"}, {"oidc_flows", "hash", "verifier"},
}

func secretScope(table, id, column string) string { return table + "/" + id + "/" + column }
func (s *Store) sealSecret(value, scope string) (string, error) {
	if s.secrets == nil {
		return "", errors.New("Verschlüsselung nicht initialisiert")
	}
	nonce := make([]byte, s.secrets.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := s.secrets.Seal(nonce, nonce, []byte(value), []byte(scope))
	return secretPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}
func (s *Store) openSecret(value, scope string) (string, error) {
	if s.secrets == nil || !strings.HasPrefix(value, secretPrefix) {
		return "", errors.New("Ungültiger verschlüsselter Datenbankwert")
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, secretPrefix))
	if err != nil || len(data) < s.secrets.NonceSize() {
		return "", errors.New("Ungültiger verschlüsselter Datenbankwert")
	}
	n := s.secrets.NonceSize()
	plain, err := s.secrets.Open(nil, data[:n], data[n:], []byte(scope))
	if err != nil {
		return "", errors.New("Datenbank-Geheimnisse nicht entschlüsselbar: Schlüssel prüfen")
	}
	return string(plain), nil
}
func (s *Store) initializeSecrets(dir string) error {
	// Before creating a key, distinguish a legacy database from a restored
	// encrypted database whose key is missing. Never replace a missing key.
	encrypted, legacy := false, false
	for _, column := range secretColumns {
		var found, plain bool
		if err := s.QueryRow("SELECT EXISTS(SELECT 1 FROM "+column.table+" WHERE "+column.column+" LIKE 'enc:%'), EXISTS(SELECT 1 FROM "+column.table+" WHERE "+column.column+" NOT LIKE 'enc:%')").Scan(&found, &plain); err != nil {
			return err
		}
		encrypted = encrypted || found
		legacy = legacy || plain
	}
	name := filepath.Join(dir, "secrets.key")
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		if encrypted {
			return errors.New("secrets.key fehlt: Datenbank zusammen mit ihrem ursprünglichen Schlüssel wiederherstellen")
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return err
		}
		file, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e == nil {
			_, e = file.Write(key)
			if e == nil {
				e = file.Sync()
			}
			closeErr := file.Close()
			if e == nil {
				e = closeErr
			}
		}
		if e != nil && !os.IsExist(e) {
			return e
		}
		if e == nil {
			if err = syncSecretsDirectory(dir); err != nil {
				return err
			}
		}
		info, err = os.Lstat(name)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != 32 {
		return errors.New("secrets.key muss eine reguläre Datei mit genau 32 Bytes sein")
	}
	if err = os.Chmod(name, 0600); err != nil {
		return err
	}
	key, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	s.secrets, err = cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if _, err = s.Exec("PRAGMA secure_delete=ON"); err != nil {
		return err
	}
	pending := filepath.Join(dir, "secrets-migration.pending")
	if legacy {
		file, createErr := os.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr == nil {
			_, createErr = file.WriteString("v1\n")
			if createErr == nil {
				createErr = file.Sync()
			}
			closeErr := file.Close()
			if createErr == nil {
				createErr = closeErr
			}
			if createErr == nil {
				createErr = syncSecretsDirectory(dir)
			}
		}
		if createErr != nil && !os.IsExist(createErr) {
			return createErr
		}
	}
	pendingInfo, pendingErr := os.Lstat(pending)
	if pendingErr != nil && !os.IsNotExist(pendingErr) {
		return pendingErr
	}
	if pendingInfo != nil && !pendingInfo.Mode().IsRegular() {
		return errors.New("Ungültige Markierung der Verschlüsselungsmigration")
	}
	changed, err := s.migrateSecrets()
	if err != nil {
		return err
	}
	if changed || pendingInfo != nil {
		// Scrub freed pages and the old WAL after the atomic conversion. Existing
		// independent backups are deliberately not modified or removed.
		if err = s.checkpointSecrets(); err != nil {
			return err
		}
		if _, err = s.Exec("VACUUM"); err != nil {
			return err
		}
		if err = s.checkpointSecrets(); err != nil {
			return err
		}
		if err = os.Remove(pending); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err = syncSecretsDirectory(dir); err != nil {
			return err
		}
	}
	return nil
}
func syncSecretsDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (s *Store) checkpointSecrets() error {
	var busy, frames, checkpointed int
	if err := s.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("Verschlüsselungsmigration benötigt exklusiven Datenbankzugriff; andere Viewer-Prozesse beenden")
	}
	return nil
}
func (s *Store) migrateSecrets() (bool, error) {
	tx, err := s.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	changed := false
	for _, column := range secretColumns {
		rows, err := tx.Query("SELECT CAST(" + column.id + " AS TEXT)," + column.column + " FROM " + column.table)
		if err != nil {
			return false, err
		}
		type entry struct{ id, value string }
		var entries []entry
		for rows.Next() {
			var e entry
			if err = rows.Scan(&e.id, &e.value); err != nil {
				rows.Close()
				return false, err
			}
			entries = append(entries, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			scope := secretScope(column.table, e.id, column.column)
			if strings.HasPrefix(e.value, "enc:") {
				if _, err = s.openSecret(e.value, scope); err != nil {
					return false, err
				}
				continue
			}
			if column.column == "payload" && !json.Valid([]byte(e.value)) {
				return false, errors.New("Ungültige bestehende Konfiguration: Verschlüsselungsmigration abgebrochen")
			}
			sealed, err := s.sealSecret(e.value, scope)
			if err != nil {
				return false, err
			}
			if _, err = tx.Exec("UPDATE "+column.table+" SET "+column.column+"=? WHERE "+column.id+"=?", sealed, e.id); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, tx.Commit()
}
func (s *Store) settingsPayload(payload string) (string, error) {
	return s.openSecret(payload, secretScope("settings", "1", "payload"))
}
func (s *Store) sealSettings(payload string) (string, error) {
	return s.sealSecret(payload, secretScope("settings", "1", "payload"))
}
