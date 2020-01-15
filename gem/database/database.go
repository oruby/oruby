package database

import (
	"database/sql"

	"github.com/oruby/oruby"
)

//import "reflect"

func init() {
	oruby.Gem("database", func(mrb *oruby.MrbState) interface{} {
		db := mrb.DefineModule("Database")

		mrb.DefineModuleFunc(db, "open", sql.Open)
		_ = mrb.DefineGoClassUnder(db, "DB", sql.Open)
		_ = mrb.DefineGoClassUnder(db, "Result", (*sql.Result)(nil))

		rows := mrb.DefineGoClassUnder(db, "Rows", (*sql.Rows)(nil))
		mrb.DefineMethod(rows, "each", rowsEach, mrb.ArgsBlock())
		mrb.DefineMethod(rows, "row", rowsRow, mrb.ArgsNone())

		_ = mrb.DefineGoClassUnder(db, "Row", (*sql.Row)(nil))
		//mrb.DefineMethod(row, "values", row_values, mrb.ARGS_NONE())

		_ = mrb.DefineGoClassUnder(db, "Stmt", (*sql.Stmt)(nil))
		_ = mrb.DefineGoClassUnder(db, "Tx", (*sql.Tx)(nil))
		return nil
	})
}

func rowsRow(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rows := mrb.Data(self).(*sql.Rows)

	if rows == nil {
		return mrb.Raise(mrb.ERuntimeError(), "Go Rows class is missing.")
	}

	cols, err := rows.Columns()
	if err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}

	dst := make([]interface{}, len(cols))
	for i := 0; i < len(dst); i++ {
		var x interface{}
		dst[i] = &x
	}

	_ = rows.Scan(dst...)

	row := mrb.HashNewCapa(len(cols))
	for i, col := range cols {
		mrb.HashSet(row, mrb.Value(col), mrb.Value(*(dst[i]).(*interface{})))
	}

	return row
}

func rowsEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	block := mrb.GetArgsBlock()
	rows := mrb.DataCheckGetInterface(self).(*sql.Rows)

	if block.IsNil() || (rows == nil) {
		return mrb.NilValue()
	}

	cols, err := rows.Columns()
	if err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}

	dst := make([]interface{}, len(cols))
	for i := 0; i < len(dst); i++ {
		var x interface{}
		dst[i] = &x
	}

	for rows.Next() {

		ai := mrb.GCArenaSave()

		err = rows.Scan(dst...)
		if err != nil {
			return mrb.Raise(mrb.ERuntimeError(), err.Error())
		}

		row := mrb.HashNewCapa(len(cols))
		for i, col := range cols {
			field := (*(dst[i]).(*interface{}))
			mrb.HashSet(row, mrb.StringValue(col), mrb.Value(field))
		}

		if !block.IsNil() {
			_, _ = mrb.YieldArgv(block, row)
		}

		mrb.GCArenaRestore(ai)
	}

	return mrb.NilValue()
}

// func InitDB(mrb *oruby.MrbState) error {

//   m  := mrb.Define_module("Database")

// //      type DB
//         // func Open(driverName, dataSourceName string) (*DB, error)
//         // func (db *DB) Begin() (*Tx, error)
//         // func (db *DB) Close() error

//     type NullBool
//         func (n *NullBool) Scan(value interface{}) error
//         func (n NullBool) ToValue() (driver.ToValue, error)
//     type NullFloat64
//         func (n *NullFloat64) Scan(value interface{}) error
//         func (n NullFloat64) ToValue() (driver.ToValue, error)
//     type NullInt64
//         func (n *NullInt64) Scan(value interface{}) error
//         func (n NullInt64) ToValue() (driver.ToValue, error)
//     type NullString
//         func (ns *NullString) Scan(value interface{}) error
//         func (ns NullString) ToValue() (driver.ToValue, error)

//   //  type Row
//   row := mrb.Define_class_under(m, "Row", mrb.ObjectClass)
//   mrb.DefineFunc(row, "scan", database.Row.Scan) // Scan(dest ...interface{}) error

//   //  type Rows
//   rows := mrb.Define_class_under(m, "Rows", mrb.ObjectClass)
//   mrb.Define_func(row, "close",   database.Rows.Close)   // Close() error
//   mrb.Define_func(row, "columns", database.Rows.Columns) // Columns() ([]string, error)
//   mrb.Define_func(row, "err",     database.Rows.Err)     // Err() error
//   mrb.Define_func(row, "next",    database.Rows.Next)    // Next() bool
//   mrb.Define_func(row, "scan",    database.Rows.Scan)    // Scan(dest ...interface{}) error

//   // type Scanner
