package control

import "testing"

func TestResolveManualLocation(t *testing.T) {
	tests := []struct {
		name              string
		locationName      string
		fallbackLatitude  float64
		fallbackLongitude float64
		wantLatitude      float64
		wantLongitude     float64
	}{
		{name: "Chinese location description", locationName: "中国 上海机房", wantLatitude: 31.2304, wantLongitude: 121.4737},
		{name: "English node name", locationName: "TXY-Shanghai", wantLatitude: 31.2304, wantLongitude: 121.4737},
		{name: "English city", locationName: "Guangzhou IDC", wantLatitude: 23.1291, wantLongitude: 113.2644},
		{name: "unknown keeps observed coordinates", locationName: "自定义边缘机房", fallbackLatitude: 22.5431, fallbackLongitude: 114.0579, wantLatitude: 22.5431, wantLongitude: 114.0579},
		{name: "unknown uses default coordinates", locationName: "自定义边缘机房", wantLatitude: defaultManualLocationLatitude, wantLongitude: defaultManualLocationLongitude},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			latitude, longitude := resolveManualLocation(test.locationName, test.fallbackLatitude, test.fallbackLongitude)
			if latitude != test.wantLatitude || longitude != test.wantLongitude {
				t.Fatalf("resolveManualLocation(%q) = (%v, %v), want (%v, %v)", test.locationName, latitude, longitude, test.wantLatitude, test.wantLongitude)
			}
		})
	}
}
