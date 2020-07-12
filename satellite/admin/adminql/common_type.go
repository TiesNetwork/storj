package adminql

import (
	"math"
	"strconv"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"storj.io/storj/satellite/admin/service"
)

const (
	// CursorType is a graphql type for cursor
	CursorType = "Cursor"
	// OrderEnumType is a graphql type for orderEnum
	OrderEnumType = "OrderEnum"
	// BigIntType is a graphql type for BigInt
	BigIntType = "BigInt"

	// CursorArg is argument name for cursor
	CursorArg = "cursor"
	// LimitArg is argument name for limit
	LimitArg = "limit"
	// OffsetArg is argument name for offset
	OffsetArg = "offset"
	// SearchArg is argument name for search
	SearchArg = "search"
	// OrderArg is argument name for order
	OrderArg = "order"

	// FieldCreatedAt is a field name for created at timestamp
	FieldCreatedAt = "createdAt"
	// FieldStartTime is a field name for for startTime
	FieldStartTime = "startTime"
	// FieldEndTime is a field name for for endTime
	FieldEndTime = "endTime"
	// FieldName is a field name for name
	FieldName = "name"
	// FieldPartnerID is a field name for partner ID
	FieldPartnerID = "partnerID"
	// FieldSince is a field name for since
	FieldSince = "since"
	// FieldBefore is a field name for before
	FieldBefore = "before"

	// DefaultCursorLimit is a default limit for cursor
	DefaultCursorLimit = 10
)

var orderEnum = graphql.NewEnum(graphql.EnumConfig{
	Name: OrderEnumType,
	Values: graphql.EnumValueConfigMap{
		"ASC": &graphql.EnumValueConfig{
			Value: service.OrderASC,
		},
		"DSC": &graphql.EnumValueConfig{
			Value: service.OrderDSC,
		},
	},
})

var bigInt = graphql.NewScalar(graphql.ScalarConfig{
	Name: BigIntType,
	Description: "The `BigInt` scalar type represents non-fractional signed whole numeric " +
		"values. Int can represent values between -(2^63) and 2^63 - 1. ",
	Serialize:  coerceBigInt,
	ParseValue: coerceBigInt,
	ParseLiteral: func(valueAST ast.Value) interface{} {
		switch valueAST := valueAST.(type) {
		case *ast.IntValue:
			if bigIntValue, err := strconv.ParseInt(valueAST.Value, 10, 64); err == nil {
				return bigIntValue
			}
		}
		return nil
	},
})

var cursorInputObject = func() *graphql.InputObject {
	search, order := graphql.Input(graphql.String), graphql.Input(orderEnum)
	return graphqlCursorCustomInput(CursorType, &search, &order)
}()

// graphqlCursorInput creates graphql.InputObject type needed to query with default cursor
func graphqlCursorInput() *graphql.InputObject {
	return cursorInputObject
}

// graphqlCursorCustomInput creates graphql.InputObject type needed to query with custom cursor
func graphqlCursorCustomInput(typeName string, search *graphql.Input, order *graphql.Input) *graphql.InputObject {
	fields := graphql.InputObjectConfigFieldMap{
		LimitArg: &graphql.InputObjectFieldConfig{
			Type: bigInt,
		},
		OffsetArg: &graphql.InputObjectFieldConfig{
			Type: bigInt,
		},
	}
	if nil != search {
		fields[SearchArg] = &graphql.InputObjectFieldConfig{
			Type: *search,
		}
	}
	if nil != order {
		fields[OrderArg] = &graphql.InputObjectFieldConfig{
			Type: *order,
		}
	}
	return graphql.NewInputObject(graphql.InputObjectConfig{
		Name:   typeName,
		Fields: fields,
	})
}

// customCursorFromMap creates Cursor from input args
func customCursorFromMap(args interface{}) *CustomCursor {
	switch args := args.(type) {
	case map[string]interface{}:
		c := CustomCursor{
			Limit: DefaultCursorLimit,
		}
		c.customSearch = args[SearchArg]
		if limit, set := args[LimitArg].(uint64); set {
			c.Limit = limit
		}
		if offset, set := args[LimitArg].(uint64); set {
			c.Offset = offset
		}
		c.customOrder = args[OrderArg]
		return &c
	default:
		return nil
	}
}

// cursorFromMap creates Cursor from input args
func cursorFromMap(args interface{}) *Cursor {
	return (*Cursor)(customCursorFromMap(args))
}

// CustomCursor is a common data structure for custom cursor
type CustomCursor struct {
	customSearch interface{}
	Limit        uint64
	Offset       uint64
	customOrder  interface{}
}

// Cursor is a common data structure for cursor
type Cursor CustomCursor

// Search gets a search string from cursor
func (p *Cursor) Search() string {
	if search, ok := p.customSearch.(string); ok {
		return search
	}
	return ""
}

// Order gets an order direction from cursor
func (p *Cursor) Order() service.OrderDirection {
	if order, ok := p.customOrder.(service.OrderDirection); ok && order.IsValid() {
		return order
	}
	return service.OrderASC
}

func coerceBigInt(value interface{}) interface{} {
	if nil == value {
		return nil
	}
	switch value := value.(type) {
	case bool:
		if value == true {
			return int64(1)
		}
		return int64(0)
	case *bool:
		return coerceBigInt(*value)
	case int:
		return int64(value)
	case *int:
		return coerceBigInt(*value)
	case int8:
		return int64(value)
	case *int8:
		return coerceBigInt(*value)
	case int16:
		return int64(value)
	case *int16:
		return coerceBigInt(*value)
	case int32:
		return int64(value)
	case *int32:
		return coerceBigInt(*value)
	case int64:
		return value
	case *int64:
		return coerceBigInt(*value)
	case uint:
		return int64(value)
	case *uint:
		return coerceBigInt(*value)
	case uint8:
		return int64(value)
	case *uint8:
		return coerceBigInt(*value)
	case uint16:
		return int64(value)
	case *uint16:
		return coerceBigInt(*value)
	case uint32:
		return int64(value)
	case *uint32:
		return coerceBigInt(*value)
	case uint64:
		if value > math.MaxInt64 {
			return nil
		}
		return int64(value)
	case *uint64:
		return coerceBigInt(*value)
	case float32:
		return int64(value)
	case *float32:
		return coerceBigInt(*value)
	case float64:
		if value < float64(math.MinInt64) || value > float64(math.MaxInt64) {
			return nil
		}
		return int64(value)
	case *float64:
		return coerceBigInt(*value)
	case string:
		val, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil
		}
		return val
	case *string:
		return coerceBigInt(*value)
	}

	// If the value cannot be transformed into an bigInt, return nil instead of '0'
	// to denote 'no integer found'
	return nil
}
