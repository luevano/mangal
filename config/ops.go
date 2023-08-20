package config

import (
	"github.com/spf13/viper"
)

func Set(key string, value any) error {
	viper.Set(key, value)
	return nil
}

func Get(key string) any {
	return viper.Get(key)
}

func Exists(key string) bool {
	return viper.IsSet(key)
}

func Keys() []string {
	return viper.AllKeys()
}

func Write() error {
	switch viper.WriteConfig().(type) {
	case nil, viper.ConfigFileNotFoundError:
		return nil
	default:
		return viper.SafeWriteConfig()
	}
}