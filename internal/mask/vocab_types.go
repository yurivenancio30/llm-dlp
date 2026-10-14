package mask

import "strings"

// Data types of languages and formats (SQL, pandas/numpy, Arrow, Polars, Spark, Avro, Protobuf, R).
// It is a closed FORMAT vocabulary: where a name comes with a data type beside it, the name is a
// column (schema reader), and a data type is never an object name.

var dataTypes = setOf(
	// SQL
	"int", "integer", "bigint", "smallint", "tinyint", "mediumint", "decimal", "numeric", "number", "float", "real",
	"double", "money", "smallmoney", "bit", "boolean", "bool", "char", "nchar", "varchar", "nvarchar", "varchar2",
	"nvarchar2", "character", "varying", "text", "ntext", "clob", "nclob", "blob", "binary", "varbinary", "bytea",
	"date", "time", "datetime", "datetime2", "smalldatetime", "timestamp", "timestamptz", "timestamp_ntz",
	"timestamp_ltz", "timestamp_tz", "interval", "uuid", "uniqueidentifier", "json", "jsonb", "xml", "variant",
	"object", "array", "geography", "geometry", "serial", "bigserial", "smallserial", "int2", "int4", "int8",
	"float4", "float8", "string", "bytes", "struct", "map", "record", "enum", "set", "long", "short", "byte",
	"int64", "int32", "int16", "uint8", "uint16", "uint32", "uint64", "float16", "float32", "float64", "complex64",
	"complex128", "bool_", "category", "datetime64", "timedelta64", "period", "sparse", "boolean[pyarrow]",
	// Arrow / Polars
	"utf8", "large_string", "large_utf8", "large_binary", "str", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
	"f32", "f64", "date32", "date64", "time32", "time64", "decimal128", "decimal256", "list", "large_list",
	"dictionary", "null", "duration", "categorical", "cat", "object_",
	// Spark
	"stringtype", "integertype", "longtype", "doubletype", "floattype", "booleantype", "datetype", "timestamptype",
	"decimaltype", "binarytype", "shorttype", "bytetype", "arraytype", "maptype", "structtype", "nulltype",
	// Avro / Protobuf
	"fixed", "sint32", "sint64", "fixed32", "fixed64", "sfixed32", "sfixed64", "uint", "repeated", "optional",
	// R / tibble
	"chr", "dbl", "lgl", "fct", "dttm", "num", "logi", "factor", "character", "numeric", "complex",
)

// isDataType: v is a data type, with or without parameters and markers (varchar(50),
// datetime64[ns], <chr>, Int64, decimal(10,2), "string?", list<int>).
func isDataType(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 48 {
		return false
	}
	v = strings.TrimSuffix(strings.TrimPrefix(v, "<"), ">")
	v = strings.TrimRight(v, "?!")
	if i := strings.IndexAny(v, "([<"); i > 0 {
		v = v[:i]
	}
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimSuffix(v, "()")
	return dataTypes[v]
}
