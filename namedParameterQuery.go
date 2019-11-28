// Adaptation of package https://github.com/Knetic/go-namedParameterQuery/ to support $ and : arg syntaxes in query
/*
	Provides support for named parameters in SQL queries used by Go / golang programs and libraries.
	Named parameters are not supported by all SQL query engines, and their standards are scattered.
	But positional parameters have wide adoption across all databases.
	npq package translates SQL queries which use named parameters into queries which use positional parameters.
	Example usage:
		query := NewNamedParameterQuery("
			SELECT * FROM table
			WHERE col1 = :foo
		")
		query.SetValue("foo", "bar")
		connection, _ := sql.Open("mysql", "user:pass@tcp(localhost:3306)/db")
		connection.QueryRow(query.GetParsedQuery(), (query.GetParsedParameters())...)
	In the example above, note the format of "QueryRow". In order to use named parameter queries,
	you will need to use npq exact format, including the variadic symbol "..."
	Note that the example above uses "QueryRow", but named parameters used in npq fashion
	work equally well for "Query" and "Exec".
	It's also possible to pass in a map, instead of defining each parameter individually:
		query := NewNamedParameterQuery("
			SELECT * FROM table
			WHERE col1 = :foo
			AND col2 IN(:firstName, :middleName, :lastName)
		")
		var parameterMap = map[string]interface{} {
			"foo": 		"bar",
			"firstName": 	"Alice",
			"lastName": 	"Bob"
			"middleName": 	"Eve",
		}
		query.SetValuesFromMap(parameterMap)
		connection, _ := sql.Open("mysql", "user:pass@tcp(localhost:3306)/db")
		connection.QueryRow(query.GetParsedQuery(), (query.GetParsedParameters())...)
	But of course, sometimes you just want to pass in an entire struct. No problem:
		type QueryValues struct {
			Foo string		`sqlParameterName:"foo"`
			FirstName string 	`sqlParameterName:"firstName"`
			MiddleName string `sqlParameterName:"middleName"`
			LastName string 	`sqlParameterName:"lirstName"`
		}
		query := NewNamedParameterQuery("
			SELECT * FROM table
			WHERE col1 = :foo
			AND col2 IN(:firstName, :middleName, :lastName)
		")
		parameter = new(QueryValues)
		query.SetValuesFromStruct(parameter)
		connection, _ := sql.Open("mysql", "user:pass@tcp(localhost:3306)/db")
		connection.QueryRow(query.GetParsedQuery(), (query.GetParsedParameters())...)
*/
package namedParameterQuery

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"unicode"
	"unicode/utf8"
)

/*
	NamedParameterQuery handles the translation of named parameters to positional parameters, for SQL statements.
	It is not recommended to create zero-valued NamedParameterQuery objects by yourself;
	instead use NewNamedParameterQuery
*/
type NamedParameterQuery struct {
	// A map of parameter names as keys, with value as a slice of positional indices which match
	// that parameter.
	positions map[string][]int

	// Contains all positional parameters, in order, ready to be used in the positional query.
	parameters []interface{}

	// The query containing named parameters, as passed in by NewNamedParameterQuery
	originalQuery string

	// The query containing positional parameters, as generated by setQuery
	revisedQuery string

	// Replace arg
	replaceArg string
}

/*
	NewNamedParameterQuery creates a new named parameter query using the given [queryText] as a SQL query which
	contains named parameters. Named parameters are identified by starting with a ":"
	e.g., ":name" refers to the parameter "name", and ":foo" refers to the parameter "foo".
	Except for their names, named parameters follow all the same rules as positional parameters;
	they cannot be inside quoted strings, and cannot inject statements into a query. They can only
	be used to insert values.
*/
func NewNamedParameterQuery(queryText string, argIndication string) *NamedParameterQuery {

	var ret *NamedParameterQuery

	// TODO: I don't like using a map for such a small amount of elements.
	// If npq becomes a bottleneck for anyone, the first thing to do would
	// be to make a slice and search routine for parameter positions.
	ret = new(NamedParameterQuery)
	ret.positions = make(map[string][]int, 8)
	ret.replaceArg = argIndication
	ret.setQuery(queryText)

	return ret
}

/*
	setQuery parses out all named parameters, stores their locations, and
	builds a "revised" query which uses positional parameters.
*/
func (npq *NamedParameterQuery) setQuery(queryText string) {

	var revisedBuilder bytes.Buffer
	var parameterBuilder bytes.Buffer
	var position []int
	var character rune
	var parameterName string
	var width int
	var positionIndex int
	var nbParameter = 0

	npq.originalQuery = queryText
	positionIndex = 0

	for i := 0; i < len(queryText); {

		character, width = utf8.DecodeRuneInString(queryText[i:])
		i += width

		// if it's a colon, do not write to builder, but grab name
		if character == ':' {

			for ; ; {

				character, width = utf8.DecodeRuneInString(queryText[i:])
				i += width

				if unicode.IsLetter(character) || unicode.IsDigit(character) {
					parameterBuilder.WriteString(string(character))
				} else {
					break
				}
			}

			// add to positions
			parameterName = parameterBuilder.String()
			nbParameter++
			position = npq.positions[parameterName]
			npq.positions[parameterName] = append(position, positionIndex)
			positionIndex++

			if npq.replaceArg == ":" {
				revisedBuilder.WriteString(":" + parameterName)
			} else if npq.replaceArg == "$" {
				revisedBuilder.WriteString(fmt.Sprintf("%s%d", npq.replaceArg, nbParameter))
			} else {
				revisedBuilder.WriteString("?")
			}

			parameterBuilder.Reset()

			if width <= 0 {
				break
			}
		}

		// otherwise write.
		revisedBuilder.WriteString(string(character))

		// if it's a quote, continue writing to builder, but do not search for parameters.
		if character == '\'' {

			for ; ; {

				character, width = utf8.DecodeRuneInString(queryText[i:])
				i += width
				revisedBuilder.WriteString(string(character))

				if character == '\'' {
					break
				}
			}
		}
	}

	npq.revisedQuery = revisedBuilder.String()
	npq.parameters = make([]interface{}, positionIndex)
}

/*
	GetParsedQuery returns a version of the original query text
	whose named parameters have been replaced by positional parameters.
*/
func (npq *NamedParameterQuery) GetParsedQuery() string {
	return npq.revisedQuery
}

/*
	GetParsedParameters returns an array of parameter objects that match the positional parameter list
	from GetParsedQuery
*/
func (npq *NamedParameterQuery) GetParsedParameters() []interface{} {
	return npq.parameters
}

/*
	SetValue sets the value of the given [parameterName] to the given [parameterValue].
	If the parsed query does not have a placeholder for the given [parameterName],
	npq method does nothing.
*/
func (npq *NamedParameterQuery) SetValue(parameterName string, parameterValue interface{}) {

	for _, position := range npq.positions[parameterName] {
		npq.parameters[position] = parameterValue
	}
}

/*
	SetValuesFromMap uses every key/value pair in the given [parameters] as a parameter replacement
	for npq query. npq is equivalent to calling SetValue for every key/value pair
	in the given [parameters] map.
	If there are any keys/values present in the map that aren't part of the query,
	they are ignored.
*/
func (npq *NamedParameterQuery) SetValuesFromMap(parameters map[string]interface{}) {

	for name, value := range parameters {
		npq.SetValue(name, value)
	}
}

/*
	SetValuesFromStruct uses reflection to find every public field of the given struct [parameters]
	and set their key/value as named parameters in npq query.
	If the given [parameters] is not a struct, npq will return an error.
	If you do not wish for a field in the struct to be added by its literal name,
	The struct may optionally specify the sqlParameterName as a tag on the field.
	e.g., a struct field may say something like:
		type Test struct {
			Foo string `sqlParameterName:"foobar"`
		}
*/
func (npq *NamedParameterQuery) SetValuesFromStruct(parameters interface{}) error {

	var fieldValues reflect.Value
	var fieldValue reflect.Value
	var parameterType reflect.Type
	var parameterField reflect.StructField
	var queryTag string
	var visibilityCharacter rune

	fieldValues = reflect.ValueOf(parameters)

	if fieldValues.Kind() != reflect.Struct {
		return errors.New("unable to add query values from parameter: parameter is not a struct")
	}

	parameterType = fieldValues.Type()

	for i := 0; i < fieldValues.NumField(); i++ {

		fieldValue = fieldValues.Field(i)
		parameterField = parameterType.Field(i)

		// public field?
		visibilityCharacter, _ = utf8.DecodeRuneInString(parameterField.Name[0:])

		if fieldValue.CanSet() || unicode.IsUpper(visibilityCharacter) {

			// check to see if npq has a tag indicating a different query name
			queryTag = parameterField.Tag.Get("sqlParameterName")

			// otherwise just add the struct's name.
			if len(queryTag) <= 0 {
				queryTag = parameterField.Name
			}

			npq.SetValue(queryTag, fieldValue.Interface())
		}
	}
	return nil
}
