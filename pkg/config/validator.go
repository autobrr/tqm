package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

/* Credits: https://sosedoff.com/2016/07/16/golang-struct-tags.html */

const (
	tagName = "validate"
)

type Validator interface {
	Validate(reflect.Value) (bool, error)
}

/* Validators */

// - Default

type DefaultValidator struct {
}

func (v DefaultValidator) Validate(val reflect.Value) (bool, error) {
	return true, nil
}

// - Required

type RequiredValidator struct {
}

func (v RequiredValidator) Validate(val reflect.Value) (bool, error) {
	if val.IsNil() {
		return false, errors.New("required setting not set")
	}
	return true, nil
}

/* Private */

func getValidatorFromTag(tag string) Validator {
	args := strings.Split(tag, ",")

	switch args[0] {
	case "required":
		return RequiredValidator{}
	}

	return DefaultValidator{}
}

/* Public */

func ValidateStruct(s interface{}) []error {
	var errs []error

	// ValueOf returns a Value representing the run-time data
	v := reflect.ValueOf(s)

	for i := 0; i < v.NumField(); i++ {
		// Get the field tag value
		tag := v.Type().Field(i).Tag.Get(tagName)

		// Skip if tag is not defined or ignored
		if tag == "" || tag == "-" {
			continue
		}

		// Get a validator that corresponds to a tag
		validator := getValidatorFromTag(tag)

		// Perform validation
		valid, err := validator.Validate(v.Field(i))

		// Append error to results
		if !valid && err != nil {
			errs = append(errs, fmt.Errorf("%q: %s", v.Type().Field(i).Name, err.Error()))
		}
	}

	return errs
}
