package sqlite

import "encoding/json"

func jsonForDB(value map[string]any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
