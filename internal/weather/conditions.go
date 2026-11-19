package weather

// shortNames maps WeatherAPI.com condition codes to at most four Han
// characters. The board's second header line has 212 px at 13 px, which fits
// four CJK glyphs next to the lunar label and the progress count; longer text
// is dropped by the layout's degradation rule.
//
// Source: https://www.weatherapi.com/docs/weather_conditions.json (60 codes).
// The API's own lang=zh wording is a sentence for the "patchy …" codes, so this
// table is keyed by the machine-readable code and uses the customary short
// Chinese forecast vocabulary.
var shortNames = map[int]string{
	1000: "晴",   // Sunny / Clear
	1003: "多云",  // Partly cloudy
	1006: "阴",   // Cloudy
	1009: "阴",   // Overcast
	1012: "霾",   // Haze
	1015: "浮尘",  // Dust haze
	1018: "扬沙",  // Blowing dust
	1021: "沙尘暴", // Dust storm
	1024: "沙尘暴", // Sandstorm
	1027: "强沙尘", // Severe sandstorm
	1030: "薄雾",  // Mist
	1033: "烟",   // Smoke
	1036: "烟霾",  // Smoky haze
	1039: "雾霾",  // Smog
	1042: "重雾霾", // Severe smog
	1045: "浮尘",  // Saharan dust
	1048: "浮尘",  // Dust
	1063: "阵雨",  // Patchy rain possible
	1066: "阵雪",  // Patchy snow possible
	1069: "雨夹雪", // Patchy sleet possible
	1072: "冻雨",  // Patchy freezing drizzle possible
	1087: "雷阵雨", // Thundery outbreaks possible
	1114: "吹雪",  // Blowing snow
	1117: "暴雪",  // Blizzard
	1135: "雾",   // Fog
	1147: "冻雾",  // Freezing fog
	1150: "毛毛雨", // Patchy light drizzle
	1153: "毛毛雨", // Light drizzle
	1168: "冻雨",  // Freezing drizzle
	1171: "冻雨",  // Heavy freezing drizzle
	1180: "阵雨",  // Patchy light rain
	1183: "小雨",  // Light rain
	1186: "阵雨",  // Moderate rain at times
	1189: "中雨",  // Moderate rain
	1192: "阵雨",  // Heavy rain at times
	1195: "大雨",  // Heavy rain
	1198: "冻雨",  // Light freezing rain
	1201: "冻雨",  // Moderate or heavy freezing rain
	1204: "雨夹雪", // Light sleet
	1207: "雨夹雪", // Moderate or heavy sleet
	1210: "阵雪",  // Patchy light snow
	1213: "小雪",  // Light snow
	1216: "阵雪",  // Patchy moderate snow
	1219: "中雪",  // Moderate snow
	1222: "阵雪",  // Patchy heavy snow
	1225: "大雪",  // Heavy snow
	1237: "冰粒",  // Ice pellets
	1240: "阵雨",  // Light rain shower
	1243: "阵雨",  // Moderate or heavy rain shower
	1246: "暴雨",  // Torrential rain shower
	1249: "雨夹雪", // Light sleet showers
	1252: "雨夹雪", // Moderate or heavy sleet showers
	1255: "阵雪",  // Light snow showers
	1258: "阵雪",  // Moderate or heavy snow showers
	1261: "冰粒",  // Light showers of ice pellets
	1264: "冰粒",  // Moderate or heavy showers of ice pellets
	1273: "雷阵雨", // Patchy light rain with thunder
	1276: "雷雨",  // Moderate or heavy rain with thunder
	1279: "雷阵雪", // Patchy light snow with thunder
	1282: "雷阵雪", // Moderate or heavy snow with thunder
}
