package migrate

import (
	"errors"
	"testing"
)

func TestDetectVersion(t *testing.T) {
	t.Parallel()

	v1 := schemaFacts{Devices: true, DeviceTelemetry: true, TelemetryBatteryPC: true}
	v2 := schemaFacts{Devices: true, DeviceTelemetry: true, TelemetryData: true}
	v3 := v2
	v3.Hosts = true
	v4 := v3
	v4.Countries = true

	cases := []struct {
		name    string
		facts   schemaFacts
		want    int64
		wantErr bool
	}{
		{"v1 original", v1, 1, false},
		{"v2 telemetry jsonb", v2, 2, false},
		{"v3 hosts", v3, 3, false},
		{"v4 countries", v4, 4, false},
		{"empty database", schemaFacts{}, 0, true},
		{"countries without hosts", schemaFacts{Devices: true, DeviceTelemetry: true, TelemetryData: true, Countries: true}, 0, true},
		{"hosts with old telemetry", schemaFacts{Devices: true, DeviceTelemetry: true, TelemetryBatteryPC: true, Hosts: true}, 0, true},
		{"telemetry with both shapes", schemaFacts{Devices: true, DeviceTelemetry: true, TelemetryData: true, TelemetryBatteryPC: true}, 0, true},
		{"telemetry with neither shape", schemaFacts{Devices: true, DeviceTelemetry: true}, 0, true},
		{"devices only", schemaFacts{Devices: true}, 0, true},
		{"telemetry only", schemaFacts{DeviceTelemetry: true, TelemetryData: true}, 0, true},
		{"only countries", schemaFacts{Countries: true}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := detectVersion(tc.facts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %t", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("version = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDetectVersionEmptyIsDistinct(t *testing.T) {
	t.Parallel()

	_, err := detectVersion(schemaFacts{})
	if !errors.Is(err, errEmptyDatabase) {
		t.Errorf("err = %v, want errEmptyDatabase", err)
	}
}
