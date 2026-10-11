package config

import (
	"fmt"
	"reflect"

	"github.com/spf13/cast"
	"github.com/spf13/viper"
)

// fields are all the registered entries by key, the config as a flattened map.
var fields = make(map[string]field)

// field is an entry without its types.
type field interface {
	description() string
	secret() bool
	isMap() bool
	kind() Kind
	values() []string
	defaultRaw() any
	// toRaw converts a value (raw, typed or a string) to the raw value stored in the file,
	// and validates it.
	toRaw(a any) (any, error)
}

// entry is a config key. Raw is the type stored in the file (string, bool, int64 or
// map[string]any), Value what the code uses.
type entry[Raw, Value any] struct {
	Key         string
	Description string
	Default     Value
	// Secret values aren't given to templates (getConfig).
	Secret bool
	// Values are the only accepted values, for enums.
	Values    []string
	Unmarshal func(Raw) (Value, error)
	Marshal   func(Value) (Raw, error)
	Validate  func(Value) error
}

// Get returns the value of the key. Every value is validated at startup (Validate) and by
// Set, so it can't be invalid here; if it is anyway the default is returned.
func (e *entry[Raw, Value]) Get() Value {
	value, err := e.decode(viper.Get(e.Key))
	if err != nil {
		return e.Default
	}
	return value
}

// Set sets the value for this run, it isn't written to the file (see Write).
func (e *entry[Raw, Value]) Set(value Value) error {
	return Set(e.Key, value)
}

// Write sets the value and writes it to the config file, see WriteKey.
func (e *entry[Raw, Value]) Write(value Value) error {
	return WriteKey(e.Key, value)
}

// raw converts a raw, typed or string (env, flags) value to Raw.
func (e *entry[Raw, Value]) raw(a any) (Raw, error) {
	if raw, ok := a.(Raw); ok {
		return raw, nil
	}
	if value, ok := a.(Value); ok {
		return e.Marshal(value)
	}
	return coerce[Raw](a)
}

func (e *entry[Raw, Value]) decode(a any) (Value, error) {
	raw, err := e.raw(a)
	if err != nil {
		var zero Value
		return zero, err
	}
	return e.Unmarshal(raw)
}

func (e *entry[Raw, Value]) toRaw(a any) (any, error) {
	raw, err := e.raw(a)
	if err != nil {
		return nil, err
	}
	value, err := e.Unmarshal(raw)
	if err != nil {
		return nil, err
	}
	if err := e.Validate(value); err != nil {
		return nil, err
	}
	// the raw value as given, e.g. a path keeps its ~
	return raw, nil
}

func (e *entry[Raw, Value]) description() string { return e.Description }
func (e *entry[Raw, Value]) secret() bool        { return e.Secret }

func (e *entry[Raw, Value]) values() []string { return e.Values }

func (e *entry[Raw, Value]) kind() Kind {
	switch any(*new(Raw)).(type) {
	case bool:
		return KindBool
	case int64:
		return KindInt
	case map[string]any:
		return KindMap
	default:
		return KindString
	}
}

func (e *entry[Raw, Value]) isMap() bool {
	_, ok := any(*new(Raw)).(map[string]any)
	return ok
}

func (e *entry[Raw, Value]) defaultRaw() any {
	raw, _ := e.Marshal(e.Default)
	return raw
}

// coerce converts a to Raw, e.g. "5" to int64 or a JSON object to a map.
func coerce[Raw any](a any) (Raw, error) {
	var raw Raw
	var out any
	var err error
	switch any(raw).(type) {
	case string:
		out, err = cast.ToStringE(a)
	case bool:
		out, err = cast.ToBoolE(a)
	case int64:
		out, err = cast.ToInt64E(a)
	case map[string]any:
		out, err = cast.ToStringMapE(a)
	default:
		return raw, fmt.Errorf("unsupported type %T", raw)
	}
	if err != nil {
		return raw, fmt.Errorf("%v (%T) isn't a %T", a, a, raw)
	}
	return out.(Raw), nil
}

// reg registers a config entry and returns it.
//
// Marshal and Unmarshal can be nil only when Raw and Value are the same type.
func reg[Raw, Value any](e entry[Raw, Value]) *entry[Raw, Value] {
	if _, dup := fields[e.Key]; dup {
		panic(fmt.Sprintf("config key %q registered twice", e.Key))
	}
	if e.Marshal == nil || e.Unmarshal == nil {
		if reflect.TypeFor[Raw]() != reflect.TypeFor[Value]() {
			panic(fmt.Sprintf("config key %q: no Marshal/Unmarshal from %T to %T", e.Key, *new(Raw), *new(Value)))
		}
		if e.Marshal == nil {
			e.Marshal = func(value Value) (Raw, error) { return any(value).(Raw), nil }
		}
		if e.Unmarshal == nil {
			e.Unmarshal = func(raw Raw) (Value, error) { return any(raw).(Value), nil }
		}
	}
	if e.Validate == nil {
		e.Validate = func(Value) error { return nil }
	}
	if _, err := e.toRaw(e.Default); err != nil {
		panic(fmt.Sprintf("config key %q: invalid default: %s", e.Key, err))
	}
	fields[e.Key] = &e
	return &e
}
