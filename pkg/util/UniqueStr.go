package util

func UniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, v := range input {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
