package domain

import (
	"errors"
	"regexp"
	"strings"
)

var containerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func ValidateContainerName(name string) (string, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if !containerNamePattern.MatchString(name) {
		return "", errors.New("请选择有效的容器名称；旧任务缺少名称时请重新选择容器")
	}
	return name, nil
}
