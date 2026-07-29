package main

import (
	"strings"
	"unicode"
)

type privacyElementMetadata struct {
	Name         string
	AutomationID string
	ClassName    string
	HelpText     string
	AriaRole     string
	AriaProps    string
	ProcessName  string
	WindowTitle  string
}

// privacyMetadataIsSensitive classifies only the focused input element. It
// deliberately avoids searching arbitrary page text: browser accessibility
// trees may expose a large part of the current document as the element name.
func privacyMetadataIsSensitive(meta privacyElementMetadata) bool {
	process := strings.ToLower(meta.ProcessName)
	browser := strings.Contains(process, "chrome") || strings.Contains(process, "msedge") ||
		strings.Contains(process, "firefox") || strings.Contains(process, "opera") ||
		strings.Contains(process, "brave") || strings.Contains(process, "vivaldi") ||
		strings.Contains(process, "browser")

	labels := shortPrivacyLabels(meta.Name, meta.HelpText)
	ids := normalizePrivacyText(strings.Join([]string{
		meta.AutomationID, meta.ClassName, meta.AriaRole, meta.AriaProps,
	}, " "))

	if browser && (privacyLabelMatchesAny(labels,
		"address and search bar", "search or enter", "search google", "type a url",
		"address bar", "omnibox", "urlbar", "searchbox", "адресная строка",
		"поиск или введите адрес", "поисковый запрос", "введите адрес", "найти на странице",
	) || privacyIDMatchesAny(ids, "omnibox", "urlbar", "address bar", "searchbox")) {
		return false
	}

	passwordTerms := []string{
		"password", "passwd", "passcode", "current password", "new password",
		"confirm password", "пароль", "текущий пароль", "новый пароль",
		"подтвердите пароль", "type password", "input type password",
		"autocomplete current password", "autocomplete new password",
	}
	otpTerms := []string{
		"otp", "totp", "2fa", "mfa", "one time password", "one time code",
		"verification code", "authentication code", "security code", "recovery code",
		"sms code", "confirmation code", "код подтверждения", "код из смс",
		"одноразовый код", "код аутентификации", "резервный код", "секретный код",
		"autocomplete one time code",
	}
	cardTerms := []string{
		"card number", "credit card number", "debit card number", "payment card number",
		"номер банковской карты", "номер карты", "cc number", "ccnumber",
		"autocomplete cc number",
	}
	cvvTerms := []string{
		"cvv", "cvc", "cvn", "csc", "card verification value", "card verification code",
		"card security code", "security code", "payment security code", "код безопасности карты",
		"код cvc", "код cvv", "cc csc", "security-code", "autocomplete cc csc",
	}
	pinTerms := []string{
		"pin", "pin code", "pincode", "bank pin", "card pin", "пин", "пин код",
		"банковский пин", "пин карты",
	}
	otherTerms := []string{
		"iban", "swift code", "routing number", "bank account number", "номер счета",
		"passport number", "passport series", "номер паспорта", "серия паспорта",
		"snils", "снилс", "tax id", "inn number", "инн",
		"secret key", "api key", "private key", "access token", "refresh token",
		"секретный ключ", "ключ доступа", "токен доступа",
	}

	groups := [][]string{passwordTerms, otpTerms, cardTerms, cvvTerms, pinTerms, otherTerms}
	for _, terms := range groups {
		if privacyLabelMatchesAny(labels, terms...) || privacyIDMatchesAny(ids, terms...) {
			return true
		}
	}
	return false
}

// shortPrivacyLabels accepts only compact labels/help text. Long names are
// commonly the full text of a web document or chat message and must never be
// used as a privacy signal.
func shortPrivacyLabels(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		n := normalizePrivacyText(value)
		if n == "" {
			continue
		}
		if len([]rune(n)) > 72 || len(strings.Fields(n)) > 10 {
			continue
		}
		result = append(result, n)
	}
	return result
}

// privacyLabelMatchesAny intentionally requires an exact field-like label.
// This avoids matching phrases mentioned inside ordinary text, for example a
// chat message discussing a "код подтверждения".
func privacyLabelMatchesAny(labels []string, terms ...string) bool {
	for _, label := range labels {
		for _, term := range terms {
			t := normalizePrivacyText(term)
			if t == "" {
				continue
			}
			if label == t ||
				label == "enter "+t || label == "type "+t || label == "confirm "+t ||
				label == "введите "+t || label == "укажите "+t || label == "подтвердите "+t ||
				label == t+" required" || label == t+" обязательное поле" {
				return true
			}
		}
	}
	return false
}

// IDs and ARIA properties are machine metadata, so a term may appear as one
// complete token sequence inside them. Boundary matching prevents "код" from
// matching words such as "кодировщик".
func privacyIDMatchesAny(value string, terms ...string) bool {
	padded := " " + value + " "
	for _, term := range terms {
		t := normalizePrivacyText(term)
		if t != "" && strings.Contains(padded, " "+t+" ") {
			return true
		}
	}
	return false
}

func normalizePrivacyText(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	b.Grow(len(value))
	lastSpace := true
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func containsAnyPrivacy(value string, terms ...string) bool {
	return privacyIDMatchesAny(normalizePrivacyText(value), terms...)
}
