package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestTelemetryData_payload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   TelemetryData
		want map[string]any
	}{
		{
			name: "legacy with all fields",
			in: TelemetryData{
				BatteryPct:       ptr(80.5),
				BatteryHealthPct: ptr(95.0),
				TempCelsius:      ptr(30.2),
				VoltageV:         ptr(4.1),
				CycleCount:       ptr(120),
				IsCharging:       ptr(true),
				CPUUsage:         ptr(12.5),
				RAMUsage:         ptr(40.5),
			},
			want: map[string]any{
				"battery_pct":        80.5,
				"battery_health_pct": 95.0,
				"temp_celsius":       30.2,
				"voltage_v":          4.1,
				"cycle_count":        120,
				"is_charging":        true,
				"cpu_usage":          12.5,
				"ram_usage":          40.5,
			},
		},
		{
			name: "legacy with missing fields filled with zeros",
			in:   TelemetryData{BatteryPct: ptr(80.0), CPUUsage: ptr(12.0)},
			want: map[string]any{
				"battery_pct":        80.0,
				"battery_health_pct": 0.0,
				"temp_celsius":       0.0,
				"voltage_v":          0.0,
				"cycle_count":        0,
				"is_charging":        false,
				"cpu_usage":          12.0,
				"ram_usage":          0.0,
			},
		},
		{
			name: "data only",
			in:   TelemetryData{Data: map[string]any{"storage_free_gb": 12.5}},
			want: map[string]any{"storage_free_gb": 12.5},
		},
		{
			name: "data plus legacy fields merged",
			in: TelemetryData{
				Data:       map[string]any{"storage_free_gb": 12.5},
				BatteryPct: ptr(80.0),
				IsCharging: ptr(false),
			},
			want: map[string]any{
				"storage_free_gb": 12.5,
				"battery_pct":     80.0,
				"is_charging":     false,
			},
		},
		{
			name: "data wins on conflict",
			in: TelemetryData{
				Data:       map[string]any{"battery_pct": 50.0},
				BatteryPct: ptr(80.0),
				CPUUsage:   ptr(12.0),
			},
			want: map[string]any{"battery_pct": 50.0, "cpu_usage": 12.0},
		},
		{
			name: "empty data without legacy fields",
			in:   TelemetryData{Data: map[string]any{}},
			want: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.in.payload(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("payload() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTelemetryData_payloadJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   TelemetryData
		want string
	}{
		{
			name: "legacy record",
			in:   TelemetryData{BatteryPct: ptr(80.5), IsCharging: ptr(true)},
			want: `{"battery_health_pct":0,"battery_pct":80.5,"cpu_usage":0,"cycle_count":0,` +
				`"is_charging":true,"ram_usage":0,"temp_celsius":0,"voltage_v":0}`,
		},
		{
			name: "mixed record",
			in: TelemetryData{
				Data:     map[string]any{"storage_free_gb": 12.5, "tags": []any{"a"}},
				CPUUsage: ptr(12.0),
			},
			want: `{"cpu_usage":12,"storage_free_gb":12.5,"tags":["a"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tt.in.payload())
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTelemetryData_payloadDoesNotMutateData(t *testing.T) {
	t.Parallel()

	data := map[string]any{"storage_free_gb": 12.5}
	m := TelemetryData{Data: data, BatteryPct: ptr(80.0)}

	got := m.payload()
	got["extra"] = true

	want := map[string]any{"storage_free_gb": 12.5}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("request data = %#v, want %#v", data, want)
	}
}
