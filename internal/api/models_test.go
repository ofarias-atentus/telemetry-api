package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestDeviceInventory_attributes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   DeviceInventory
		want map[string]any
	}{
		{
			name: "legacy with all fields",
			in: DeviceInventory{
				AliasName:        ptr("a"),
				Model:            ptr("m"),
				OSVersion:        ptr("14"),
				HostLocation:     ptr("lab"),
				BatteryDesignCap: ptr(4000),
			},
			want: map[string]any{
				"alias_name": "a", "model": "m", "os_version": "14", "battery_design_cap": 4000,
			},
		},
		{
			name: "legacy with missing fields filled with zeros",
			in:   DeviceInventory{Model: ptr("m")},
			want: map[string]any{
				"alias_name": "", "model": "m", "os_version": "", "battery_design_cap": 0,
			},
		},
		{
			name: "attributes only",
			in:   DeviceInventory{Attributes: map[string]any{"sim": "y"}},
			want: map[string]any{"sim": "y"},
		},
		{
			name: "attributes plus legacy fields merged",
			in: DeviceInventory{
				Attributes: map[string]any{"sim": "y"},
				AliasName:  ptr("a"),
			},
			want: map[string]any{"sim": "y", "alias_name": "a"},
		},
		{
			name: "attributes win on conflict",
			in: DeviceInventory{
				Attributes: map[string]any{"alias_name": "from-attributes"},
				AliasName:  ptr("from-legacy"),
				Model:      ptr("m"),
			},
			want: map[string]any{"alias_name": "from-attributes", "model": "m"},
		},
		{
			name: "empty attributes",
			in:   DeviceInventory{Attributes: map[string]any{}},
			want: map[string]any{},
		},
		{
			name: "null legacy field is zero-filled in legacy mode",
			in:   DeviceInventory{AliasName: nil, BatteryDesignCap: ptr(10)},
			want: map[string]any{
				"alias_name": "", "model": "", "os_version": "", "battery_design_cap": 10,
			},
		},
		{
			name: "null legacy field is absent in attributes mode",
			in:   DeviceInventory{Attributes: map[string]any{"x": 1}, AliasName: nil},
			want: map[string]any{"x": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.in.attributes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("attributes() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDeviceInventory_attributesIgnoresHostLocation(t *testing.T) {
	t.Parallel()

	d := DeviceInventory{Attributes: map[string]any{}, HostLocation: ptr("lab")}
	if got := d.attributes(); len(got) != 0 {
		t.Errorf("attributes() = %#v, want empty", got)
	}
}

func TestDeviceInventory_hostLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   *string
		want *string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: ptr(""), want: nil},
		{name: "set", in: ptr("lab"), want: ptr("lab")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DeviceInventory{HostLocation: tt.in}.hostLocation()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("hostLocation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeviceInventory_attributesJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   DeviceInventory
		want string
	}{
		{
			name: "legacy record",
			in:   DeviceInventory{AliasName: ptr("a"), BatteryDesignCap: ptr(4000)},
			want: `{"alias_name":"a","battery_design_cap":4000,"model":"","os_version":""}`,
		},
		{
			name: "mixed record",
			in: DeviceInventory{
				Attributes: map[string]any{"sim": "y", "tags": []any{"a"}},
				Model:      ptr("m"),
			},
			want: `{"model":"m","sim":"y","tags":["a"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tt.in.attributes())
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDeviceInventory_attributesDoesNotMutateAttributes(t *testing.T) {
	t.Parallel()

	attrs := map[string]any{"sim": "y"}
	d := DeviceInventory{Attributes: attrs, AliasName: ptr("a")}

	got := d.attributes()
	got["extra"] = true

	want := map[string]any{"sim": "y"}
	if !reflect.DeepEqual(attrs, want) {
		t.Errorf("request attributes = %#v, want %#v", attrs, want)
	}
}

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
