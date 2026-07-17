package control

import (
	"strings"
	"unicode"
)

const (
	defaultManualLocationLatitude  = 35.8617
	defaultManualLocationLongitude = 104.1954
)

type manualLocationPreset struct {
	aliases   []string
	latitude  float64
	longitude float64
}

var manualLocationPresets = []manualLocationPreset{
	{aliases: []string{"上海", "上海市", "shanghai"}, latitude: 31.2304, longitude: 121.4737},
	{aliases: []string{"广州", "广州市", "guangzhou", "canton"}, latitude: 23.1291, longitude: 113.2644},
	{aliases: []string{"北京", "北京市", "beijing", "peking"}, latitude: 39.9042, longitude: 116.4074},
	{aliases: []string{"深圳", "深圳市", "shenzhen"}, latitude: 22.5431, longitude: 114.0579},
	{aliases: []string{"成都", "成都市", "chengdu"}, latitude: 30.5728, longitude: 104.0668},
	{aliases: []string{"重庆", "重庆市", "chongqing"}, latitude: 29.5630, longitude: 106.5516},
	{aliases: []string{"杭州", "杭州市", "hangzhou"}, latitude: 30.2741, longitude: 120.1551},
	{aliases: []string{"南京", "南京市", "nanjing"}, latitude: 32.0603, longitude: 118.7969},
	{aliases: []string{"武汉", "武汉市", "wuhan"}, latitude: 30.5928, longitude: 114.3055},
	{aliases: []string{"西安", "西安市", "xian", "xi'an"}, latitude: 34.3416, longitude: 108.9398},
	{aliases: []string{"天津", "天津市", "tianjin"}, latitude: 39.0851, longitude: 117.1994},
	{aliases: []string{"苏州", "苏州市", "suzhou"}, latitude: 31.2989, longitude: 120.5853},
	{aliases: []string{"青岛", "青岛市", "qingdao", "tsingtao"}, latitude: 36.0671, longitude: 120.3826},
	{aliases: []string{"厦门", "厦门市", "xiamen", "amoy"}, latitude: 24.4798, longitude: 118.0894},
	{aliases: []string{"长沙", "长沙市", "changsha"}, latitude: 28.2282, longitude: 112.9388},
	{aliases: []string{"郑州", "郑州市", "zhengzhou"}, latitude: 34.7466, longitude: 113.6254},
	{aliases: []string{"济南", "济南市", "jinan"}, latitude: 36.6512, longitude: 117.1201},
	{aliases: []string{"福州", "福州市", "fuzhou"}, latitude: 26.0745, longitude: 119.2965},
	{aliases: []string{"昆明", "昆明市", "kunming"}, latitude: 25.0389, longitude: 102.7183},
	{aliases: []string{"贵阳", "贵阳市", "guiyang"}, latitude: 26.6470, longitude: 106.6302},
	{aliases: []string{"南宁", "南宁市", "nanning"}, latitude: 22.8170, longitude: 108.3665},
	{aliases: []string{"海口", "海口市", "haikou"}, latitude: 20.0440, longitude: 110.1999},
	{aliases: []string{"兰州", "兰州市", "lanzhou"}, latitude: 36.0611, longitude: 103.8343},
	{aliases: []string{"乌鲁木齐", "乌鲁木齐市", "urumqi", "wulumuqi"}, latitude: 43.8256, longitude: 87.6168},
	{aliases: []string{"拉萨", "拉萨市", "lhasa"}, latitude: 29.6520, longitude: 91.1721},
	{aliases: []string{"香港", "香港特别行政区", "hongkong"}, latitude: 22.3193, longitude: 114.1694},
	{aliases: []string{"澳门", "澳门特别行政区", "macao", "macau"}, latitude: 22.1987, longitude: 113.5439},
	{aliases: []string{"台北", "台北市", "taipei"}, latitude: 25.0330, longitude: 121.5654},
	{aliases: []string{"东京", "tokyo"}, latitude: 35.6762, longitude: 139.6503},
	{aliases: []string{"大阪", "osaka"}, latitude: 34.6937, longitude: 135.5023},
	{aliases: []string{"新加坡", "singapore"}, latitude: 1.3521, longitude: 103.8198},
	{aliases: []string{"首尔", "汉城", "seoul"}, latitude: 37.5665, longitude: 126.9780},
	{aliases: []string{"曼谷", "bangkok"}, latitude: 13.7563, longitude: 100.5018},
	{aliases: []string{"吉隆坡", "kualalumpur"}, latitude: 3.1390, longitude: 101.6869},
	{aliases: []string{"伦敦", "london"}, latitude: 51.5074, longitude: -0.1278},
	{aliases: []string{"法兰克福", "frankfurt"}, latitude: 50.1109, longitude: 8.6821},
	{aliases: []string{"巴黎", "paris"}, latitude: 48.8566, longitude: 2.3522},
	{aliases: []string{"阿姆斯特丹", "amsterdam"}, latitude: 52.3676, longitude: 4.9041},
	{aliases: []string{"莫斯科", "moscow"}, latitude: 55.7558, longitude: 37.6173},
	{aliases: []string{"纽约", "newyork"}, latitude: 40.7128, longitude: -74.0060},
	{aliases: []string{"洛杉矶", "losangeles"}, latitude: 34.0522, longitude: -118.2437},
	{aliases: []string{"旧金山", "三藩市", "sanfrancisco"}, latitude: 37.7749, longitude: -122.4194},
	{aliases: []string{"西雅图", "seattle"}, latitude: 47.6062, longitude: -122.3321},
	{aliases: []string{"芝加哥", "chicago"}, latitude: 41.8781, longitude: -87.6298},
	{aliases: []string{"多伦多", "toronto"}, latitude: 43.6532, longitude: -79.3832},
	{aliases: []string{"温哥华", "vancouver"}, latitude: 49.2827, longitude: -123.1207},
	{aliases: []string{"悉尼", "sydney"}, latitude: -33.8688, longitude: 151.2093},
	{aliases: []string{"墨尔本", "melbourne"}, latitude: -37.8136, longitude: 144.9631},
	{aliases: []string{"圣保罗", "saopaulo"}, latitude: -23.5505, longitude: -46.6333},
}

func normalizeManualLocationName(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func resolveManualLocation(name string, fallbackLatitude, fallbackLongitude float64) (float64, float64) {
	normalized := normalizeManualLocationName(name)
	for _, preset := range manualLocationPresets {
		for _, alias := range preset.aliases {
			normalizedAlias := normalizeManualLocationName(alias)
			if normalized == normalizedAlias || strings.Contains(normalized, normalizedAlias) {
				return preset.latitude, preset.longitude
			}
		}
	}
	if validAutomaticLocationCoordinates(fallbackLatitude, fallbackLongitude) {
		return fallbackLatitude, fallbackLongitude
	}
	return defaultManualLocationLatitude, defaultManualLocationLongitude
}
