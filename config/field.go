package config

import (
	"fmt"
	"reflect"
)

// fields are all the registered fields from the config entries,
// essentially a config as a flattened map
var fields = make(map[string]field)

type field struct {
	Default     any
	Description string
	Unmarshal   func(any) (any, error)
	Marshal     func(any) (any, error)
	Validate    func(any) error
}

type entry[Raw, Value any] struct {
	Key         string
	Description string
	Default     Value
	Unmarshal   func(Raw) (Value, error)
	Marshal     func(Value) (Raw, error)
	Validate    func(Value) error
}

func (e *entry[Raw, Value]) Get() Value {
	v := Get(e.Key)
	// check Raw first, when Raw and Value are the same type Unmarshal still needs to run (e.g. expanding paths)
	raw, ok := v.(Raw)
	if !ok {
		return v.(Value)
	}
	value, err := e.Unmarshal(raw)
	if err != nil {
		panic(fmt.Errorf("config entry Get: error unmarshaling raw value %v (%T) from key %q: %s", v, v, e.Key, err.Error()))
	}
	return value
}

func (e *entry[Raw, Value]) Set(value Value) error {
	raw, err := e.Marshal(value)
	if err != nil {
		return fmt.Errorf("config entry Set: error marshaling value %v (%T) from key %q: %s", value, value, e.Key, err.Error())
	}
	return Set(e.Key, raw)
}

// Register a new config entry.
//
// Returns itself as a pointer for later access through Config.
func reg[Raw, Value any](e entry[Raw, Value]) *entry[Raw, Value] {
	if e.Marshal == nil {
		e.Marshal = func(value Value) (raw Raw, err error) {
			return reflect.
				ValueOf(value).
				Convert(reflect.ValueOf(raw).Type()).
				Interface().(Raw), nil
		}
	}
	if e.Unmarshal == nil {
		e.Unmarshal = func(raw Raw) (value Value, err error) {
			return reflect.
				ValueOf(raw).
				Convert(reflect.ValueOf(value).Type()).
				Interface().(Value), nil
		}
	}
	if e.Validate == nil {
		e.Validate = func(value Value) error {
			return nil
		}
	}

	fields[e.Key] = field{
		Description: e.Description,
		Default:     e.Default,
		Marshal: func(a any) (any, error) {
			raw, ok := a.(Raw)
			if !ok {
				return e.Marshal(a.(Value))
			}
			return raw, nil
		},
		Unmarshal: func(a any) (any, error) {
			raw, ok := a.(Raw)
			if !ok {
				return a.(Value), nil
			}
			return e.Unmarshal(raw)
		},
		Validate: func(a any) error {
			raw, ok := a.(Raw)
			if !ok {
				return e.Validate(a.(Value))
			}
			value, err := e.Unmarshal(raw)
			if err != nil {
				return fmt.Errorf("error validating field %q: wrong type of value: expected %T, got %T", e.Key, *new(Value), a)
			}
			return e.Validate(value)
		},
	}
	// Default must be set after the field has been "registered"
	// as SetDefault validates the value by a Fields map lookup
	if err := SetDefault(e.Key, e.Default); err != nil {
		panic(fmt.Errorf("error setting default value for key %q: %s", e.Key, err.Error()))
	}
	return &e
}
