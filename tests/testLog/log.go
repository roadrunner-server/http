package testLog //nolint: stylecheck

import (
	"go.uber.org/zap"
)

func ZapLogger() *zap.Logger {
	l, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}

	return l
}
