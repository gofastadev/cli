package validators

import (
	"reflect"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

func getValue(fl validator.FieldLevel) string {
	var value string
	if fl.Field().Kind() == reflect.Ptr {
		if fl.Field().IsNil() {
			return ""
		}
		// Dereference pointer to get the value
		field := fl.Field().Elem()

		// Handle UUID fields separately
		if field.Type() == reflect.TypeOf(uuid.UUID{}) {
			value = field.Interface().(uuid.UUID).String()
		} else {
			value = field.String()
		}
	} else {
		// Handle UUID fields separately for non-pointer values
		if fl.Field().Type() == reflect.TypeOf(uuid.UUID{}) {
			value = fl.Field().Interface().(uuid.UUID).String()
		} else {
			value = fl.Field().String()
		}
	}

	return value
}
