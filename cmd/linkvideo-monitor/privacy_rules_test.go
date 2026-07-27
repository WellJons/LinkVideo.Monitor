package main

import "testing"

func TestPrivacyRulesExcludeBrowserChromeAndOrdinaryFields(t *testing.T) {
	cases := []privacyElementMetadata{
		{Name: "Address and search bar", ClassName: "OmniboxViewViews", ProcessName: "chrome.exe"},
		{Name: "Search Google or type a URL", AutomationID: "view_1001", ProcessName: "msedge.exe"},
		{Name: "Поиск", AutomationID: "searchbox", ProcessName: "firefox.exe"},
		{Name: "Электронная почта", AutomationID: "email", ProcessName: "chrome.exe"},
		{Name: "Комментарий", AutomationID: "message", ProcessName: "chrome.exe"},
		{Name: "Как нужно", AutomationID: "chat-input", ProcessName: "chrome.exe"},
		{Name: "подпись", AutomationID: "editor", ProcessName: "chrome.exe"},
		{Name: "совпадение", AutomationID: "editor", ProcessName: "chrome.exe"},
		{Name: "Кодировщик Intel Quick Sync", AutomationID: "editor", ProcessName: "chrome.exe"},
		{Name: "В этом сообщении обсуждается код подтверждения и номер банковской карты", AutomationID: "chat-input", ProcessName: "chrome.exe"},
	}
	for _, tc := range cases {
		if privacyMetadataIsSensitive(tc) {
			t.Fatalf("ordinary browser field was marked sensitive: %+v", tc)
		}
	}
}

func TestPrivacyRulesDetectStrongSensitiveFields(t *testing.T) {
	cases := []privacyElementMetadata{
		{Name: "Password", AutomationID: "password", ProcessName: "chrome.exe"},
		{Name: "Введите код подтверждения", AutomationID: "otp", ProcessName: "msedge.exe"},
		{Name: "CVV", HelpText: "Card security code", ProcessName: "chrome.exe"},
		{Name: "Номер банковской карты", AutomationID: "card-number", ProcessName: "firefox.exe"},
		{Name: "API key", AutomationID: "secret_key", ProcessName: "app.exe"},
		{AutomationID: "checkout-cc-number", AriaProps: "autocomplete=cc-number", ProcessName: "chrome.exe"},
		{AutomationID: "verification-code", AriaProps: "autocomplete=one-time-code", ProcessName: "chrome.exe"},
	}
	for _, tc := range cases {
		if !privacyMetadataIsSensitive(tc) {
			t.Fatalf("sensitive field was not detected: %+v", tc)
		}
	}
}

func TestPrivacyRulesUseBoundaries(t *testing.T) {
	cases := []privacyElementMetadata{
		{Name: "Кодировщик", AutomationID: "codec", ProcessName: "chrome.exe"},
		{Name: "Pinterest", AutomationID: "pinboard", ProcessName: "chrome.exe"},
		{Name: "Карточка товара", AutomationID: "product-card", ProcessName: "chrome.exe"},
	}
	for _, tc := range cases {
		if privacyMetadataIsSensitive(tc) {
			t.Fatalf("partial word caused a false positive: %+v", tc)
		}
	}
}
