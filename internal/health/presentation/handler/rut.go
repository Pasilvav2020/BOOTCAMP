package handler

import (
	"errors"
	"strings"
)

func normalizeChileanRUT(value string) (string, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return "", errors.New("RUT vacío")
	}

	var body string
	var verifier string
	if strings.Contains(value, "-") {
		if strings.Count(value, "-") != 1 {
			return "", errors.New("formato de RUT inválido")
		}
		parts := strings.SplitN(value, "-", 2)
		body, verifier = parts[0], parts[1]
		if len(verifier) != 1 {
			return "", errors.New("dígito verificador inválido")
		}
	} else {
		if len(value) < 2 {
			return "", errors.New("RUT incompleto")
		}
		body, verifier = value[:len(value)-1], value[len(value)-1:]
	}

	if strings.Contains(body, ".") {
		groups := strings.Split(body, ".")
		if len(groups[0]) < 1 || len(groups[0]) > 3 {
			return "", errors.New("formato de RUT inválido")
		}
		for i, group := range groups {
			if (i > 0 && len(group) != 3) || !isASCIIDigits(group) {
				return "", errors.New("formato de RUT inválido")
			}
		}
		body = strings.Join(groups, "")
	} else if !isASCIIDigits(body) {
		return "", errors.New("cuerpo del RUT inválido")
	}

	if strings.Trim(body, "0") == "" {
		return "", errors.New("cuerpo del RUT debe ser positivo")
	}

	if len(body) > 8 || len(verifier) != 1 || (!isASCIIDigits(verifier) && verifier != "K") {
		return "", errors.New("formato de RUT inválido")
	}

	sum := 0
	weight := 2
	for i := len(body) - 1; i >= 0; i-- {
		sum += int(body[i]-'0') * weight
		weight++
		if weight > 7 {
			weight = 2
		}
	}

	expectedVerifier := 11 - (sum % 11)
	switch expectedVerifier {
	case 11:
		expectedVerifier = 0
	case 10:
		if verifier != "K" {
			return "", errors.New("dígito verificador inválido")
		}
		return body + "-K", nil
	}
	if verifier != string(rune('0'+expectedVerifier)) {
		return "", errors.New("dígito verificador inválido")
	}

	return body + "-" + verifier, nil
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
