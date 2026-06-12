package rootaika

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const dbTimeLayout = "2006-01-02T15:04:05.000000000Z"

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	if path == "" {
		path = "rootaika.db"
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.seed(ctx, time.Now().UTC()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  created_at_utc TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_uuid TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  registration_status TEXT NOT NULL DEFAULT 'unassigned',
  created_at_utc TEXT NOT NULL,
  last_seen_at_utc TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_uuid TEXT NOT NULL UNIQUE,
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  state TEXT NOT NULL,
  occurred_at_utc TEXT NOT NULL,
  process_name TEXT NOT NULL DEFAULT '',
  sequence INTEGER NOT NULL DEFAULT 0,
  received_at_utc TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_device_time_seq_idx ON events(device_id, occurred_at_utc, sequence);

CREATE TABLE IF NOT EXISTS device_config (
  device_id INTEGER PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
  idle_threshold_seconds INTEGER NOT NULL,
  upload_interval_seconds INTEGER NOT NULL,
  poll_interval_seconds INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS commands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at_utc TEXT NOT NULL,
  ack_at_utc TEXT
);
CREATE INDEX IF NOT EXISTS commands_device_status_idx ON commands(device_id, status, created_at_utc);

CREATE TABLE IF NOT EXISTS program_categories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  match_type TEXT NOT NULL,
  pattern TEXT NOT NULL,
  category TEXT NOT NULL,
  created_at_utc TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS program_categories_unique_idx ON program_categories(match_type, pattern, category);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at_utc TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_credentials (
  role TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_plaintext TEXT NOT NULL,
  updated_at_utc TEXT NOT NULL
);
`)
	return err
}

func (s *Store) seed(ctx context.Context, now time.Time) error {
	defaults := map[string]string{
		"idle_threshold_seconds":    "60",
		"upload_interval_seconds":   "60",
		"poll_interval_seconds":     "30",
		"max_countable_gap_seconds": "300",
	}
	for key, value := range defaults {
		if _, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO settings(key, value, updated_at_utc) VALUES (?, ?, ?)`,
			key, value, formatDBTime(now)); err != nil {
			return err
		}
	}

	if err := s.seedCredential(ctx, now, RoleAdmin, "admin", "admin", "ROOTAIKA_ADMIN_USER", "ROOTAIKA_ADMIN_PASSWORD"); err != nil {
		return err
	}
	return s.seedCredential(ctx, now, RoleClient, "client", "client", "ROOTAIKA_CLIENT_USER", "ROOTAIKA_CLIENT_PASSWORD")
}

func (s *Store) seedCredential(ctx context.Context, now time.Time, role Role, defaultUser, defaultPassword, userEnv, passwordEnv string) error {
	username, userSet := os.LookupEnv(userEnv)
	password, passwordSet := os.LookupEnv(passwordEnv)
	if !userSet {
		username = defaultUser
	}
	if !passwordSet {
		password = defaultPassword
	}

	if userSet || passwordSet {
		_, err := s.db.ExecContext(ctx, `
INSERT INTO auth_credentials(role, username, password_plaintext, updated_at_utc)
VALUES (?, ?, ?, ?)
ON CONFLICT(role) DO UPDATE SET
  username = excluded.username,
  password_plaintext = excluded.password_plaintext,
  updated_at_utc = excluded.updated_at_utc`,
			string(role), username, password, formatDBTime(now))
		return err
	}

	_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO auth_credentials(role, username, password_plaintext, updated_at_utc)
VALUES (?, ?, ?, ?)`,
		string(role), username, password, formatDBTime(now))
	return err
}

func (s *Store) Credentials(ctx context.Context) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role, username, password_plaintext FROM auth_credentials`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var credentials []Credential
	for rows.Next() {
		var role, username, password string
		if err := rows.Scan(&role, &username, &password); err != nil {
			return nil, err
		}
		credentials = append(credentials, Credential{Role: Role(role), Username: username, Password: password})
	}
	return credentials, rows.Err()
}

func (s *Store) EnsureDevice(ctx context.Context, clientUUID string, now time.Time) (Device, error) {
	clientUUID = strings.TrimSpace(clientUUID)
	if clientUUID == "" {
		return Device{}, errors.New("client_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()

	nowText := formatDBTime(now)
	result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO devices(client_uuid, display_name, registration_status, created_at_utc, last_seen_at_utc)
VALUES (?, ?, 'unassigned', ?, ?)`,
		clientUUID, defaultDeviceName(clientUUID), nowText, nowText)
	if err != nil {
		return Device{}, err
	}

	device, err := scanDeviceRow(tx.QueryRowContext(ctx, `
SELECT d.id, d.client_uuid, d.display_name, d.user_id, COALESCE(u.name, ''),
       d.registration_status, d.created_at_utc, d.last_seen_at_utc
FROM devices d
LEFT JOIN users u ON u.id = d.user_id
WHERE d.client_uuid = ?`, clientUUID))
	if err != nil {
		return Device{}, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at_utc = ? WHERE id = ?`, nowText, device.ID); err != nil {
		return Device{}, err
	}
	device.LastSeenAt = now.UTC()

	affected, _ := result.RowsAffected()
	if affected > 0 {
		settings, err := settingsFromTx(ctx, tx)
		if err != nil {
			return Device{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO device_config(device_id, idle_threshold_seconds, upload_interval_seconds, poll_interval_seconds)
VALUES (?, ?, ?, ?)`,
			device.ID, settings.IdleThresholdSeconds, settings.UploadIntervalSeconds, settings.PollIntervalSeconds); err != nil {
			return Device{}, err
		}
	} else {
		if err := ensureDeviceConfigTx(ctx, tx, device.ID); err != nil {
			return Device{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Device{}, err
	}
	return device, nil
}

func (s *Store) InsertEvents(ctx context.Context, deviceID int64, events []EventInput, now time.Time) (int, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	accepted := 0
	ignored := 0
	receivedAt := formatDBTime(now)
	for _, event := range events {
		processName := event.ProcessName
		if event.State != StateActive {
			processName = ""
		}
		result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO events(event_uuid, device_id, type, state, occurred_at_utc, process_name, sequence, received_at_utc)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			event.EventUUID, deviceID, event.Type, event.State, formatDBTime(event.OccurredAt), processName, event.Sequence, receivedAt)
		if err != nil {
			return 0, 0, err
		}
		rows, _ := result.RowsAffected()
		if rows == 1 {
			accepted++
		} else {
			ignored++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return accepted, ignored, nil
}

func (s *Store) ClientConfig(ctx context.Context, clientUUID string, now time.Time) (ClientConfig, error) {
	device, err := s.EnsureDevice(ctx, clientUUID, now)
	if err != nil {
		return ClientConfig{}, err
	}

	var config ClientConfig
	config.DeviceID = device.ID
	err = s.db.QueryRowContext(ctx, `
SELECT idle_threshold_seconds, upload_interval_seconds, poll_interval_seconds
FROM device_config WHERE device_id = ?`, device.ID).
		Scan(&config.IdleThresholdSeconds, &config.UploadIntervalSeconds, &config.PollIntervalSeconds)
	if err != nil {
		return ClientConfig{}, err
	}

	settings, err := s.Settings(ctx)
	if err != nil {
		return ClientConfig{}, err
	}
	config.MaxCountableGapSeconds = settings.MaxCountableGapSeconds

	categories, err := s.Categories(ctx)
	if err != nil {
		return ClientConfig{}, err
	}
	config.Categories = categories
	return config, nil
}

func (s *Store) PendingCommands(ctx context.Context, clientUUID string, now time.Time) ([]Command, error) {
	device, err := s.EnsureDevice(ctx, clientUUID, now)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, device_id, type, status, created_at_utc, ack_at_utc
FROM commands
WHERE device_id = ? AND status = 'pending'
ORDER BY created_at_utc ASC, id ASC`, device.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

func (s *Store) AckCommand(ctx context.Context, commandID int64, clientUUID string, now time.Time) (bool, error) {
	var deviceID int64
	var err error
	if strings.TrimSpace(clientUUID) != "" {
		device, err := s.EnsureDevice(ctx, clientUUID, now)
		if err != nil {
			return false, err
		}
		deviceID = device.ID
	}

	query := `UPDATE commands SET status = 'acked', ack_at_utc = ? WHERE id = ?`
	args := []any{formatDBTime(now), commandID}
	if deviceID != 0 {
		query += ` AND device_id = ?`
		args = append(args, deviceID)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		return true, nil
	}

	existsQuery := `SELECT COUNT(*) FROM commands WHERE id = ? AND status = 'acked'`
	existsArgs := []any{commandID}
	if deviceID != 0 {
		existsQuery += ` AND device_id = ?`
		existsArgs = append(existsArgs, deviceID)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, existsQuery, existsArgs...).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) CreateCommand(ctx context.Context, deviceID int64, commandType string, now time.Time) (int64, error) {
	if !validCommand(commandType) {
		return 0, fmt.Errorf("invalid command type %q", commandType)
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO commands(device_id, type, status, created_at_utc)
VALUES (?, ?, 'pending', ?)`, deviceID, commandType, formatDBTime(now))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT d.id, d.client_uuid, d.display_name, d.user_id, COALESCE(u.name, ''),
       d.registration_status, d.created_at_utc, d.last_seen_at_utc
FROM devices d
LEFT JOIN users u ON u.id = d.user_id
ORDER BY d.display_name COLLATE NOCASE, d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		device, err := scanDeviceRows(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at_utc FROM users ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		var created string
		if err := rows.Scan(&user.ID, &user.Name, &created); err != nil {
			return nil, err
		}
		user.CreatedAt = parseDBTime(created)
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) CreateUser(ctx context.Context, name string, now time.Time) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name is required")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users(name, created_at_utc) VALUES (?, ?)
ON CONFLICT(name) DO NOTHING`, name, formatDBTime(now))
	return err
}

func (s *Store) UpdateDevice(ctx context.Context, deviceID int64, displayName string, userID *int64) error {
	displayName = strings.TrimSpace(displayName)
	status := "unassigned"
	if userID != nil {
		status = "assigned"
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE devices SET display_name = ?, user_id = ?, registration_status = ? WHERE id = ?`,
		displayName, nullableInt64(userID), status, deviceID)
	return err
}

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	return settingsFromDB(ctx, s.db)
}

func (s *Store) UpdateSettings(ctx context.Context, settings Settings, now time.Time) error {
	if settings.IdleThresholdSeconds <= 0 || settings.UploadIntervalSeconds <= 0 ||
		settings.PollIntervalSeconds <= 0 || settings.MaxCountableGapSeconds <= 0 {
		return errors.New("settings must be positive integers")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	values := map[string]int{
		"idle_threshold_seconds":    settings.IdleThresholdSeconds,
		"upload_interval_seconds":   settings.UploadIntervalSeconds,
		"poll_interval_seconds":     settings.PollIntervalSeconds,
		"max_countable_gap_seconds": settings.MaxCountableGapSeconds,
	}
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO settings(key, value, updated_at_utc) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at_utc = excluded.updated_at_utc`,
			key, strconv.Itoa(value), formatDBTime(now)); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE device_config
SET idle_threshold_seconds = ?, upload_interval_seconds = ?, poll_interval_seconds = ?`,
		settings.IdleThresholdSeconds, settings.UploadIntervalSeconds, settings.PollIntervalSeconds); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Categories(ctx context.Context) ([]ProgramCategory, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, match_type, pattern, category, created_at_utc
FROM program_categories
ORDER BY category COLLATE NOCASE, pattern COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []ProgramCategory
	for rows.Next() {
		var category ProgramCategory
		var created string
		if err := rows.Scan(&category.ID, &category.MatchType, &category.Pattern, &category.Category, &created); err != nil {
			return nil, err
		}
		category.CreatedAt = parseDBTime(created)
		categories = append(categories, category)
	}
	return categories, rows.Err()
}

func (s *Store) CreateCategory(ctx context.Context, matchType, pattern, category string, now time.Time) error {
	matchType = strings.TrimSpace(matchType)
	pattern = strings.TrimSpace(pattern)
	category = strings.TrimSpace(category)
	if matchType == "" || pattern == "" || category == "" {
		return errors.New("match_type, pattern and category are required")
	}
	if matchType != "exact" && matchType != "prefix" && matchType != "contains" {
		return errors.New("match_type must be exact, prefix or contains")
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO program_categories(match_type, pattern, category, created_at_utc)
VALUES (?, ?, ?, ?)
ON CONFLICT(match_type, pattern, category) DO UPDATE SET category = excluded.category`,
		matchType, pattern, category, formatDBTime(now))
	return err
}

func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM program_categories WHERE id = ?`, id)
	return err
}

func (s *Store) RecentCommands(ctx context.Context, limit int) ([]Command, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.device_id,
       COALESCE(NULLIF(d.display_name, ''), d.client_uuid) AS device,
       c.type, c.status, c.created_at_utc, c.ack_at_utc
FROM commands c
JOIN devices d ON d.id = c.device_id
ORDER BY c.created_at_utc DESC, c.id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCommands(rows)
}

func (s *Store) ReportEvents(ctx context.Context, deviceID int64, start, end time.Time) ([]ActivityEvent, error) {
	var events []ActivityEvent
	before := s.db.QueryRowContext(ctx, `
SELECT id, event_uuid, device_id, type, state, occurred_at_utc, process_name, sequence
FROM events
WHERE device_id = ? AND occurred_at_utc < ?
ORDER BY occurred_at_utc DESC, sequence DESC, id DESC
LIMIT 1`, deviceID, formatDBTime(start))
	if event, err := scanEventRow(before); err == nil {
		events = append(events, event)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, event_uuid, device_id, type, state, occurred_at_utc, process_name, sequence
FROM events
WHERE device_id = ? AND occurred_at_utc >= ? AND occurred_at_utc < ?
ORDER BY occurred_at_utc ASC, sequence ASC, id ASC`, deviceID, formatDBTime(start), formatDBTime(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		event, err := scanEventRows(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanDeviceRow(row *sql.Row) (Device, error) {
	var device Device
	var userID sql.NullInt64
	var created, lastSeen string
	if err := row.Scan(&device.ID, &device.ClientUUID, &device.DisplayName, &userID, &device.UserName,
		&device.RegistrationStatus, &created, &lastSeen); err != nil {
		return Device{}, err
	}
	if userID.Valid {
		device.UserID = &userID.Int64
	}
	device.CreatedAt = parseDBTime(created)
	device.LastSeenAt = parseDBTime(lastSeen)
	return device, nil
}

func scanDeviceRows(rows *sql.Rows) (Device, error) {
	var device Device
	var userID sql.NullInt64
	var created, lastSeen string
	if err := rows.Scan(&device.ID, &device.ClientUUID, &device.DisplayName, &userID, &device.UserName,
		&device.RegistrationStatus, &created, &lastSeen); err != nil {
		return Device{}, err
	}
	if userID.Valid {
		device.UserID = &userID.Int64
	}
	device.CreatedAt = parseDBTime(created)
	device.LastSeenAt = parseDBTime(lastSeen)
	return device, nil
}

func scanCommands(rows *sql.Rows) ([]Command, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var commands []Command
	for rows.Next() {
		var command Command
		var created string
		var ack sql.NullString
		switch len(columns) {
		case 6:
			if err := rows.Scan(&command.ID, &command.DeviceID, &command.Type, &command.Status, &created, &ack); err != nil {
				return nil, err
			}
		case 7:
			if err := rows.Scan(&command.ID, &command.DeviceID, &command.Device, &command.Type, &command.Status, &created, &ack); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unexpected command column count %d", len(columns))
		}
		command.CreatedAt = parseDBTime(created)
		if ack.Valid {
			ackAt := parseDBTime(ack.String)
			command.AckAt = &ackAt
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func scanEventRow(row *sql.Row) (ActivityEvent, error) {
	var event ActivityEvent
	var occurred string
	if err := row.Scan(&event.ID, &event.EventUUID, &event.DeviceID, &event.Type, &event.State,
		&occurred, &event.ProcessName, &event.Sequence); err != nil {
		return ActivityEvent{}, err
	}
	event.OccurredAt = parseDBTime(occurred)
	return event, nil
}

func scanEventRows(rows *sql.Rows) (ActivityEvent, error) {
	var event ActivityEvent
	var occurred string
	if err := rows.Scan(&event.ID, &event.EventUUID, &event.DeviceID, &event.Type, &event.State,
		&occurred, &event.ProcessName, &event.Sequence); err != nil {
		return ActivityEvent{}, err
	}
	event.OccurredAt = parseDBTime(occurred)
	return event, nil
}

func settingsFromDB(ctx context.Context, db *sql.DB) (Settings, error) {
	values := map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return Settings{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Settings{}, err
		}
		intValue, err := strconv.Atoi(value)
		if err != nil {
			return Settings{}, fmt.Errorf("setting %s is not an integer: %w", key, err)
		}
		values[key] = intValue
	}
	if err := rows.Err(); err != nil {
		return Settings{}, err
	}

	return Settings{
		IdleThresholdSeconds:   defaultInt(values["idle_threshold_seconds"], 60),
		UploadIntervalSeconds:  defaultInt(values["upload_interval_seconds"], 60),
		PollIntervalSeconds:    defaultInt(values["poll_interval_seconds"], 30),
		MaxCountableGapSeconds: defaultInt(values["max_countable_gap_seconds"], 300),
	}, nil
}

func settingsFromTx(ctx context.Context, tx *sql.Tx) (Settings, error) {
	rows, err := tx.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return Settings{}, err
	}
	defer rows.Close()

	values := map[string]int{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Settings{}, err
		}
		intValue, err := strconv.Atoi(value)
		if err != nil {
			return Settings{}, err
		}
		values[key] = intValue
	}
	return Settings{
		IdleThresholdSeconds:   defaultInt(values["idle_threshold_seconds"], 60),
		UploadIntervalSeconds:  defaultInt(values["upload_interval_seconds"], 60),
		PollIntervalSeconds:    defaultInt(values["poll_interval_seconds"], 30),
		MaxCountableGapSeconds: defaultInt(values["max_countable_gap_seconds"], 300),
	}, rows.Err()
}

func ensureDeviceConfigTx(ctx context.Context, tx *sql.Tx, deviceID int64) error {
	settings, err := settingsFromTx(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT OR IGNORE INTO device_config(device_id, idle_threshold_seconds, upload_interval_seconds, poll_interval_seconds)
VALUES (?, ?, ?, ?)`,
		deviceID, settings.IdleThresholdSeconds, settings.UploadIntervalSeconds, settings.PollIntervalSeconds)
	return err
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultDeviceName(clientUUID string) string {
	if len(clientUUID) <= 8 {
		return clientUUID
	}
	return "Laite " + clientUUID[len(clientUUID)-8:]
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func formatDBTime(t time.Time) string {
	return t.UTC().Format(dbTimeLayout)
}

func parseDBTime(value string) time.Time {
	t, err := time.Parse(dbTimeLayout, value)
	if err == nil {
		return t
	}
	t, _ = time.Parse(time.RFC3339Nano, value)
	return t
}
