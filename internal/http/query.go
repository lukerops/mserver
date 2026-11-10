package http

import (
	"fmt"
	"reflect"
	"strconv"
	"uuid"
)

func decodeParam(fieldTag string, getParam func(string) string, target any) error {
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Pointer || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("o alvo do decode precisa ser um ponteiro para uma struct")
	}

	val = val.Elem()
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		fieldVal := val.Field(i)
		fieldType := typ.Field(i)

		tag := fieldType.Tag.Get(fieldTag)
		if tag == "" || tag == "-" {
			continue
		}

		paramValue := getParam(tag)
		if paramValue == "" {
			continue // Ignora se o parâmetro não foi enviado na URL
		}

		if fieldVal.Kind() == reflect.Pointer {
			// Se o ponteiro for nil, inicializa ele com uma nova instância do tipo correto
			if fieldVal.IsNil() {
				fieldVal.Set(reflect.New(fieldVal.Type().Elem()))
			}
			// Aponta fieldVal para o elemento real (o valor apontado pelo ponteiro)
			fieldVal = fieldVal.Elem()

		}

		// Converte a string do query param para o tipo nativo do campo da struct
		switch fieldVal.Kind() {
		case reflect.String:
			fieldVal.SetString(paramValue)

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			parsedInt, err := strconv.ParseInt(paramValue, 10, 64)
			if err != nil {
				return fmt.Errorf("parâmetro '%s' inválido: esperado um número inteiro", tag)
			}
			fieldVal.SetInt(parsedInt)

		case reflect.Bool:
			parsedBool, err := strconv.ParseBool(paramValue)
			if err != nil {
				return fmt.Errorf("parâmetro '%s' inválido: esperado um valor booleano", tag)
			}
			fieldVal.SetBool(parsedBool)
		}

		switch fieldVal.Type() {
		case reflect.TypeFor[uuid.UUID]():
			parsedUUID, err := uuid.Parse(paramValue)
			if err != nil {
				return fmt.Errorf("parâmetro '%s' inválido: esperado um UUID válido", tag)
			}
			fieldVal.Set(reflect.ValueOf(parsedUUID))
		}
	}
	return nil
}
