package validators

import (
	"log/slog"
	"reflect"
	"regexp"

	"github.com/go-playground/validator/v10"
	"gorm.io/gorm"
)

// CREATE OR UPDATE
func isRecordExistByEmailForConflict(db *gorm.DB) validator.Func {
	return func(fl validator.FieldLevel) bool {
		if fl.Field().Kind() == reflect.Ptr && fl.Field().IsNil() {
			return true
		}
		email := getValue(fl)
		tableName := fl.Param()
		var count int64
		err := db.Table(tableName).Where("email = ?", email).Count(&count).Error
		if err != nil {
			slog.Error("error querying the database", "error", err)
			return false
		}
		return count == 0
	}
}
func doesRecordExistByEmailForVerification(db *gorm.DB) validator.Func {
	return func(fl validator.FieldLevel) bool {
		if fl.Field().Kind() == reflect.Ptr && fl.Field().IsNil() {
			return true
		}
		email := getValue(fl)
		tableName := fl.Param()
		var count int64
		err := db.Table(tableName).Where("email = ? AND deleted_at IS NULL", email).Count(&count).Error
		if err != nil {
			slog.Error("error querying the database", "error", err)
			return false
		}
		return count > 0
	}
}

func isRecordExistByPhoneNumberForConflict(db *gorm.DB) validator.Func {
	return func(fl validator.FieldLevel) bool {
		if fl.Field().Kind() == reflect.Ptr && fl.Field().IsNil() {
			return true
		}
		phoneNumber := getValue(fl)
		tableName := fl.Param()
		var count int64
		err := db.Table(tableName).Where("phone_number = ?", phoneNumber).Count(&count).Error
		if err != nil {
			slog.Error("error querying the database", "error", err)
			return false
		}
		return count == 0
	}
}

func isRecordExistByPhoneNumberForVerification(db *gorm.DB) validator.Func {
	return func(fl validator.FieldLevel) bool {
		if fl.Field().Kind() == reflect.Ptr && fl.Field().IsNil() {
			return true
		}
		phoneNumber := getValue(fl)
		tableName := fl.Param()
		var count int64
		err := db.Table(tableName).Where("phone_number = ?", phoneNumber).Count(&count).Error
		if err != nil {
			slog.Error("error querying the database", "error", err)
			return false
		}
		return count > 0
	}
}

func isValidPhoneNumber(fl validator.FieldLevel) bool {
	if fl.Field().Kind() == reflect.Ptr && fl.Field().IsNil() {
		return true
	}

	phoneNumber := fl.Field().String()

	regex := `^\d{6,14}$`
	matched, _ := regexp.MatchString(regex, phoneNumber)

	return matched
}
