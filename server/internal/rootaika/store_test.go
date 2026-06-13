package rootaika

import (
	"context"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
}

func TestOpenStoreSeedsDefaults(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	credentials, err := store.Credentials(ctx)
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	if len(credentials) != 2 {
		t.Fatalf("credentials len = %d", len(credentials))
	}

	roles := map[Role]Credential{}
	for _, credential := range credentials {
		roles[credential.Role] = credential
	}
	if roles[RoleAdmin].Username != "admin" || roles[RoleAdmin].Password != "admin" {
		t.Fatalf("admin credential = %+v", roles[RoleAdmin])
	}
	if roles[RoleClient].Username != "client" || roles[RoleClient].Password != "client" {
		t.Fatalf("client credential = %+v", roles[RoleClient])
	}

	settings, err := store.Settings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if settings.IdleThresholdSeconds != 60 || settings.MaxCountableGapSeconds != 300 ||
		settings.DebugMode || settings.DebugUnassignedClients {
		t.Fatalf("seeded settings = %+v", settings)
	}
}

func TestSeedCredentialEnvOverride(t *testing.T) {
	t.Setenv("ROOTAIKA_ADMIN_USER", "superadmin")
	t.Setenv("ROOTAIKA_ADMIN_PASSWORD", "s3cret")

	store, err := OpenStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	credentials, err := store.Credentials(context.Background())
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	var admin Credential
	for _, credential := range credentials {
		if credential.Role == RoleAdmin {
			admin = credential
		}
	}
	if admin.Username != "superadmin" || admin.Password != "s3cret" {
		t.Fatalf("env override not applied: %+v", admin)
	}
}

func TestOpenStoreDefaultPathEmpty(t *testing.T) {
	// Empty path defaults to a file; exercise the empty-path branch with a
	// shared in-memory DSN to avoid creating files on disk.
	store, err := OpenStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestEnsureDeviceCreatesUnassignedAndIsIdempotent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	first, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if first.RegistrationStatus != "unassigned" {
		t.Fatalf("status = %q", first.RegistrationStatus)
	}
	if first.DisplayName == "" {
		t.Fatalf("display name should be defaulted")
	}

	// device_config row must have been created.
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_config WHERE device_id = ?`, first.ID).Scan(&count); err != nil {
		t.Fatalf("config count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("device_config rows = %d", count)
	}

	later := fixedNow().Add(time.Hour)
	second, err := store.EnsureDevice(ctx, "client-1", later)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("idempotency broken: %d != %d", second.ID, first.ID)
	}
	if !second.LastSeenAt.Equal(later.UTC()) {
		t.Fatalf("last seen not updated: %v", second.LastSeenAt)
	}

	devices, err := store.Devices(ctx)
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices len = %d", len(devices))
	}
}

func TestEnsureDeviceRejectsEmptyID(t *testing.T) {
	store := testStore(t)
	if _, err := store.EnsureDevice(context.Background(), "   ", fixedNow()); err == nil {
		t.Fatalf("expected error for empty client id")
	}
}

func TestInsertEventsAcceptsAndDeduplicatesAndBlanksProcess(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	events := []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "steam.exe", OccurredAt: fixedNow(), Sequence: 1},
		{EventUUID: "b", Type: EventTypeActivityObserved, State: StateIdle, ProcessName: "ignored.exe", OccurredAt: fixedNow().Add(time.Minute), Sequence: 2},
	}
	accepted, ignored, err := store.InsertEvents(ctx, device.ID, events, fixedNow())
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if accepted != 2 || ignored != 0 {
		t.Fatalf("accepted=%d ignored=%d", accepted, ignored)
	}

	accepted, ignored, err = store.InsertEvents(ctx, device.ID, events, fixedNow())
	if err != nil {
		t.Fatalf("re-insert: %v", err)
	}
	if accepted != 0 || ignored != 2 {
		t.Fatalf("dedup accepted=%d ignored=%d", accepted, ignored)
	}

	// idle event should have process_name blanked.
	var processName string
	if err := store.db.QueryRowContext(ctx, `SELECT process_name FROM events WHERE event_uuid = 'b'`).Scan(&processName); err != nil {
		t.Fatalf("process query: %v", err)
	}
	if processName != "" {
		t.Fatalf("idle process not blanked: %q", processName)
	}
}

func TestClientConfigReturnsConfigAndCategories(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := store.CreateCategory(ctx, "exact", "steam.exe", "pelit", fixedNow()); err != nil {
		t.Fatalf("create category: %v", err)
	}

	config, err := store.ClientConfig(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("client config: %v", err)
	}
	if config.DeviceID == 0 {
		t.Fatalf("device id missing")
	}
	if config.IdleThresholdSeconds != 60 {
		t.Fatalf("idle = %d", config.IdleThresholdSeconds)
	}
	if config.MaxCountableGapSeconds != 300 {
		t.Fatalf("max gap = %d", config.MaxCountableGapSeconds)
	}
	if config.DebugMode {
		t.Fatalf("debug mode should be false")
	}
	if len(config.Categories) != 1 || config.Categories[0].Category != "pelit" {
		t.Fatalf("categories = %+v", config.Categories)
	}
}

func TestClientConfigDebugsUnassignedClientsWhenEnabled(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if err := store.UpdateSettings(ctx, Settings{
		IdleThresholdSeconds:   60,
		UploadIntervalSeconds:  60,
		PollIntervalSeconds:    30,
		MaxCountableGapSeconds: 300,
		DebugUnassignedClients: true,
	}, fixedNow()); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	config, err := store.ClientConfig(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("client config: %v", err)
	}
	if !config.DebugMode {
		t.Fatalf("unassigned client debug mode should be true")
	}

	if err := store.CreateUser(ctx, "Alice", fixedNow()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, err := store.Users(ctx)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	if err := store.UpdateDevice(ctx, config.DeviceID, "Box", &users[0].ID); err != nil {
		t.Fatalf("assign device: %v", err)
	}

	config, err = store.ClientConfig(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("client config after assign: %v", err)
	}
	if config.DebugMode {
		t.Fatalf("assigned client debug mode should follow global debug setting")
	}
}

func TestPendingCommandsOnlyReturnsPending(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	id, err := store.CreateCommand(ctx, device.ID, CommandLock, fixedNow())
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	pending, err := store.PendingCommands(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("pending = %+v", pending)
	}

	if _, err := store.AckCommand(ctx, id, "client-1", fixedNow()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	pending, err = store.PendingCommands(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("pending after ack: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after ack = %+v", pending)
	}
}

func TestCreateCommandSupersedesPending(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if _, err := store.CreateCommand(ctx, device.ID, CommandLock, fixedNow()); err != nil {
		t.Fatalf("create lock: %v", err)
	}
	// A second lock must not stack up another pending command.
	if _, err := store.CreateCommand(ctx, device.ID, CommandLock, fixedNow()); err != nil {
		t.Fatalf("create lock again: %v", err)
	}
	// An unlock supersedes the queued lock.
	unlockID, err := store.CreateCommand(ctx, device.ID, CommandUnlock, fixedNow())
	if err != nil {
		t.Fatalf("create unlock: %v", err)
	}

	pending, err := store.PendingCommands(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != unlockID || pending[0].Type != CommandUnlock {
		t.Fatalf("pending = %+v, want single unlock id=%d", pending, unlockID)
	}
}

func TestCreateCommandKeepsAckedHistory(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	lockID, err := store.CreateCommand(ctx, device.ID, CommandLock, fixedNow())
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}
	if _, err := store.AckCommand(ctx, lockID, "client-1", fixedNow()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	// Superseding only removes pending commands; acked history survives.
	if _, err := store.CreateCommand(ctx, device.ID, CommandUnlock, fixedNow()); err != nil {
		t.Fatalf("create unlock: %v", err)
	}

	all, err := store.RecentCommands(ctx, 10)
	if err != nil {
		t.Fatalf("commands: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("commands = %+v, want lock(acked)+unlock(pending)", all)
	}
}

func TestAckCommandSuccessAndIdempotent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	id, err := store.CreateCommand(ctx, device.ID, CommandLock, fixedNow())
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	ok, err := store.AckCommand(ctx, id, "client-1", fixedNow())
	if err != nil || !ok {
		t.Fatalf("first ack ok=%v err=%v", ok, err)
	}
	ok, err = store.AckCommand(ctx, id, "client-1", fixedNow())
	if err != nil || !ok {
		t.Fatalf("idempotent ack ok=%v err=%v", ok, err)
	}

	// Unknown command id returns false.
	ok, err = store.AckCommand(ctx, 99999, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ack unknown err: %v", err)
	}
	if ok {
		t.Fatalf("ack unknown should be false")
	}
}

func TestAckCommandWithoutClientID(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	id, err := store.CreateCommand(ctx, device.ID, CommandUnlock, fixedNow())
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	ok, err := store.AckCommand(ctx, id, "", fixedNow())
	if err != nil || !ok {
		t.Fatalf("ack without client ok=%v err=%v", ok, err)
	}
}

func TestCreateCommandRejectsInvalidType(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := store.CreateCommand(ctx, device.ID, "explode", fixedNow()); err == nil {
		t.Fatalf("expected error for invalid command type")
	}
}

func TestUsersCreateAndDedup(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.CreateUser(ctx, "Alice", fixedNow()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.CreateUser(ctx, "Alice", fixedNow()); err != nil {
		t.Fatalf("dedup create user: %v", err)
	}
	if err := store.CreateUser(ctx, "  ", fixedNow()); err == nil {
		t.Fatalf("expected error for empty user name")
	}

	users, err := store.Users(ctx)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	if len(users) != 1 || users[0].Name != "Alice" {
		t.Fatalf("users = %+v", users)
	}
}

func TestUpdateDeviceAssignsUserAndStatus(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := store.CreateUser(ctx, "Bob", fixedNow()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, err := store.Users(ctx)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	userID := users[0].ID

	if err := store.UpdateDevice(ctx, device.ID, "Workstation", &userID); err != nil {
		t.Fatalf("update device: %v", err)
	}
	devices, err := store.Devices(ctx)
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if devices[0].RegistrationStatus != "assigned" || devices[0].UserName != "Bob" || devices[0].DisplayName != "Workstation" {
		t.Fatalf("device after assign = %+v", devices[0])
	}

	// Unassign.
	if err := store.UpdateDevice(ctx, device.ID, "Workstation", nil); err != nil {
		t.Fatalf("unassign device: %v", err)
	}
	devices, err = store.Devices(ctx)
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if devices[0].RegistrationStatus != "unassigned" || devices[0].UserID != nil {
		t.Fatalf("device after unassign = %+v", devices[0])
	}
}

func TestDeleteDeviceRemovesDeviceAndRelatedRows(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := store.CreateUser(ctx, "Bob", fixedNow()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	users, err := store.Users(ctx)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	if err := store.UpdateDevice(ctx, device.ID, "Workstation", &users[0].ID); err != nil {
		t.Fatalf("assign device: %v", err)
	}
	if _, _, err := store.InsertEvents(ctx, device.ID, []EventInput{
		{EventUUID: "a", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "steam.exe", OccurredAt: fixedNow(), Sequence: 1},
	}, fixedNow()); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	if _, err := store.CreateCommand(ctx, device.ID, CommandLock, fixedNow()); err != nil {
		t.Fatalf("create command: %v", err)
	}

	if err := store.DeleteDevice(ctx, device.ID); err != nil {
		t.Fatalf("delete device: %v", err)
	}

	devices, err := store.Devices(ctx)
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("devices after delete = %+v", devices)
	}

	for table, query := range map[string]string{
		"events":        `SELECT COUNT(*) FROM events WHERE device_id = ?`,
		"device_config": `SELECT COUNT(*) FROM device_config WHERE device_id = ?`,
		"commands":      `SELECT COUNT(*) FROM commands WHERE device_id = ?`,
	} {
		var count int
		if err := store.db.QueryRowContext(ctx, query, device.ID).Scan(&count); err != nil {
			t.Fatalf("%s count: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after delete = %d", table, count)
		}
	}
}

func TestUpdateSettingsPersistsAndRejectsNonPositive(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	settings := Settings{
		IdleThresholdSeconds:   90,
		UploadIntervalSeconds:  120,
		PollIntervalSeconds:    45,
		MaxCountableGapSeconds: 600,
		DebugMode:              true,
		DebugUnassignedClients: true,
	}
	if err := store.UpdateSettings(ctx, settings, fixedNow()); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	got, err := store.Settings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if got != settings {
		t.Fatalf("settings = %+v want %+v", got, settings)
	}

	// device_config must reflect the new values.
	var idle, upload, poll int
	if err := store.db.QueryRowContext(ctx, `SELECT idle_threshold_seconds, upload_interval_seconds, poll_interval_seconds FROM device_config WHERE device_id = ?`, device.ID).Scan(&idle, &upload, &poll); err != nil {
		t.Fatalf("device_config query: %v", err)
	}
	if idle != 90 || upload != 120 || poll != 45 {
		t.Fatalf("device_config = %d/%d/%d", idle, upload, poll)
	}

	bad := settings
	bad.IdleThresholdSeconds = 0
	if err := store.UpdateSettings(ctx, bad, fixedNow()); err == nil {
		t.Fatalf("expected error for non-positive setting")
	}
}

func TestCategoriesCreateValidateAndDelete(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.CreateCategory(ctx, "exact", "steam.exe", "pelit", fixedNow()); err != nil {
		t.Fatalf("create exact: %v", err)
	}
	if err := store.CreateCategory(ctx, "weird", "steam.exe", "pelit", fixedNow()); err == nil {
		t.Fatalf("expected invalid match_type error")
	}
	if err := store.CreateCategory(ctx, "exact", "", "pelit", fixedNow()); err == nil {
		t.Fatalf("expected required-field error")
	}

	categories, err := store.Categories(ctx)
	if err != nil {
		t.Fatalf("categories: %v", err)
	}
	if len(categories) != 1 {
		t.Fatalf("categories len = %d", len(categories))
	}

	if err := store.DeleteCategory(ctx, categories[0].ID); err != nil {
		t.Fatalf("delete category: %v", err)
	}
	categories, err = store.Categories(ctx)
	if err != nil {
		t.Fatalf("categories after delete: %v", err)
	}
	if len(categories) != 0 {
		t.Fatalf("categories after delete = %d", len(categories))
	}
}

func TestRecentCommandsIncludesDeviceName(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := store.CreateCommand(ctx, device.ID, CommandLock, fixedNow()); err != nil {
		t.Fatalf("create command: %v", err)
	}

	// limit clamping branch: zero -> 50.
	commands, err := store.RecentCommands(ctx, 0)
	if err != nil {
		t.Fatalf("recent commands: %v", err)
	}
	if len(commands) != 1 || commands[0].Device == "" {
		t.Fatalf("recent commands = %+v", commands)
	}

	// out-of-range limit also clamps.
	if _, err := store.RecentCommands(ctx, 9999); err != nil {
		t.Fatalf("recent commands clamp: %v", err)
	}
}

func TestReportEventsIncludesCarryOverEvent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	device, err := store.EnsureDevice(ctx, "client-1", fixedNow())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	start := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	events := []EventInput{
		{EventUUID: "before", Type: EventTypeActivityObserved, State: StateActive, ProcessName: "game.exe", OccurredAt: start.Add(-10 * time.Minute), Sequence: 1},
		{EventUUID: "inside", Type: EventTypeActivityObserved, State: StateIdle, OccurredAt: start.Add(30 * time.Minute), Sequence: 2},
	}
	if _, _, err := store.InsertEvents(ctx, device.ID, events, fixedNow()); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := store.ReportEvents(ctx, device.ID, start, end)
	if err != nil {
		t.Fatalf("report events: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("report events len = %d", len(got))
	}
	if got[0].EventUUID != "before" {
		t.Fatalf("first event should be carry-over, got %q", got[0].EventUUID)
	}
}

func TestHelperFunctions(t *testing.T) {
	if defaultInt(0, 60) != 60 {
		t.Fatalf("defaultInt zero fallback failed")
	}
	if defaultInt(5, 60) != 5 {
		t.Fatalf("defaultInt passthrough failed")
	}
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatalf("boolToInt failed")
	}
	if defaultDeviceName("short") != "short" {
		t.Fatalf("defaultDeviceName short failed")
	}
	if got := defaultDeviceName("abcdefgh12345678"); got != "Laite 12345678" {
		t.Fatalf("defaultDeviceName long = %q", got)
	}
	if nullableInt64(nil) != nil {
		t.Fatalf("nullableInt64 nil failed")
	}
	v := int64(7)
	if nullableInt64(&v) != int64(7) {
		t.Fatalf("nullableInt64 value failed")
	}
}

func TestParseAndFormatDBTimeRoundTrip(t *testing.T) {
	original := time.Date(2026, 6, 11, 12, 34, 56, 123456789, time.UTC)
	formatted := formatDBTime(original)
	parsed := parseDBTime(formatted)
	if !parsed.Equal(original) {
		t.Fatalf("round trip mismatch: %v != %v", parsed, original)
	}

	// RFC3339 fallback path.
	fallback := parseDBTime("2026-06-11T12:00:00Z")
	if fallback.IsZero() {
		t.Fatalf("RFC3339 fallback failed")
	}
	// Unparseable input yields zero time.
	if !parseDBTime("not-a-time").IsZero() {
		t.Fatalf("invalid input should yield zero time")
	}
}
