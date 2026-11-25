package app

import "testing"

// 延迟校验：服务器在香港，截图里标错的两跳就是这么被检出的。
func TestRouteLocationDoubtful(t *testing.T) {
	hkLat, hkLon := 22.3193, 114.1694
	cases := []struct {
		name string
		ms   float64
		lat  float64
		lon  float64
		want bool
	}{
		{"悉尼 0.7ms", 0.7, -33.8688, 151.2093, true},
		{"新加坡 0.6ms", 0.6, 1.3521, 103.8198, true},
		{"广州 29.7ms", 29.7, 23.1291, 113.2644, false},
		{"香港 0.7ms", 0.7, 22.3193, 114.1694, false},
		// 150 公里外、0.5ms：没余量会误伤，有 200 公里余量才放过。
		{"同城 150 公里 0.5ms", 0.5, 23.6693, 114.1694, false},
		// 上海 1200 公里、5ms：上限 700 公里，到不了，确实对不上。
		{"上海 5ms", 5, 31.2304, 121.4737, true},
	}
	for _, c := range cases {
		if got := routeLocationDoubtful(hkLat, hkLon, c.ms, c.lat, c.lon); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// 服务器自身位置未知、或这一跳本来就没定位时，无法校验，不存疑。
	if routeLocationDoubtful(0, 0, 0.7, -33.8688, 151.2093) {
		t.Error("服务器位置未知时不应判定存疑")
	}
	if routeLocationDoubtful(hkLat, hkLon, 0.7, 0, 0) {
		t.Error("没有定位时不应判定存疑")
	}
}

func TestRouteDisplayHop(t *testing.T) {
	hkLat, hkLon := 22.3193, 114.1694
	// 截图里的新南威尔士州一跳：降级为只显示 IP、不上地图。
	hop := routeDisplayHop(
		tracedHop{ip: "203.0.113.10", ms: 0.7},
		ipGeo{Status: "success", City: "", RegionName: "新南威尔士州", Country: "澳大利亚", Lat: -33.8688, Lon: 151.2093},
		true, hkLat, hkLon, false,
	)
	if hop.Name != "203.0.113.10" || hop.Lat != 0 || hop.Lon != 0 || !hop.Doubt {
		t.Errorf("存疑跳未降级: %+v", hop)
	}
	// 广州一跳正常保留。
	hop = routeDisplayHop(
		tracedHop{ip: "14.17.0.1", ms: 29.7},
		ipGeo{Status: "success", City: "Guangzhou", RegionName: "Guangdong", Country: "中国", Lat: 23.1291, Lon: 113.2644},
		true, hkLat, hkLon, false,
	)
	if hop.Name != "广州" || hop.Lat != 23.1291 || hop.Lon != 113.2644 || hop.Doubt {
		t.Errorf("正常跳被误伤: %+v", hop)
	}
	// 内网跳本来就没定位：显示 IP，不标存疑。
	hop = routeDisplayHop(tracedHop{ip: "10.21.0.48", ms: 0.6}, ipGeo{}, false, hkLat, hkLon, false)
	if hop.Name != "10.21.0.48" || hop.Lat != 0 || hop.Lon != 0 || hop.Doubt {
		t.Errorf("内网跳处理不对: %+v", hop)
	}
	// 定位接口失败：显示 IP，不标存疑。
	hop = routeDisplayHop(
		tracedHop{ip: "1.2.3.4", ms: 1.0},
		ipGeo{Status: "fail"}, true, hkLat, hkLon, false,
	)
	if hop.Name != "1.2.3.4" || hop.Doubt {
		t.Errorf("定位失败跳处理不对: %+v", hop)
	}
	// 最后一跳（你的位置）即使位置对不上也始终保留。
	hop = routeDisplayHop(
		tracedHop{ip: "1.2.3.4", ms: 38.0},
		ipGeo{Status: "success", City: "Sydney", RegionName: "New South Wales", Country: "Australia", Lat: -33.8688, Lon: 151.2093},
		true, hkLat, hkLon, true,
	)
	if hop.Name != "你的位置" || hop.Lat == 0 || hop.Lon == 0 || hop.Doubt {
		t.Errorf("最后一跳不应降级: %+v", hop)
	}
}
