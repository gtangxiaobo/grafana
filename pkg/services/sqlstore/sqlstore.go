package sqlstore

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/torkelo/grafana-pro/pkg/bus"
	"github.com/torkelo/grafana-pro/pkg/log"
	m "github.com/torkelo/grafana-pro/pkg/models"
	"github.com/torkelo/grafana-pro/pkg/services/sqlstore/migrator"
	"github.com/torkelo/grafana-pro/pkg/setting"
	"github.com/torkelo/grafana-pro/pkg/util"

	_ "github.com/go-sql-driver/mysql"
	"github.com/go-xorm/xorm"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

var (
	x       *xorm.Engine
	dialect migrator.Dialect
	tables  []interface{}

	HasEngine bool

	DbCfg struct {
		Type, Host, Name, User, Pwd, Path, SslMode string
	}

	UseSQLite3 bool
)

func EnsureAdminUser() {
	adminQuery := m.GetUserByLoginQuery{LoginOrEmail: setting.AdminUser}

	if err := bus.Dispatch(&adminQuery); err == m.ErrUserNotFound {
		cmd := m.CreateUserCommand{}
		cmd.Login = setting.AdminUser
		cmd.Email = setting.AdminUser + "@localhost"
		cmd.Salt = util.GetRandomString(10)
		cmd.Rands = util.GetRandomString(10)
		cmd.Password = util.EncodePassword(setting.AdminPassword, cmd.Salt)
		cmd.IsAdmin = true

		if err = bus.Dispatch(&cmd); err != nil {
			log.Fatal(3, "Failed to create default admin user", err)
		}

		log.Info("Created default admin user: %v", setting.AdminUser)
	} else if err != nil {
		log.Fatal(3, "Could not determine if admin user exists: %v", err)
	}
}

func NewEngine() {
	x, err := getEngine()

	if err != nil {
		log.Fatal(3, "Sqlstore: Fail to connect to database: %v", err)
	}

	err = SetEngine(x, true)

	if err != nil {
		log.Fatal(3, "fail to initialize orm engine: %v", err)
	}
}

func SetEngine(engine *xorm.Engine, enableLog bool) (err error) {
	x = engine
	dialect = migrator.NewDialect(x.DriverName())

	migrator := migrator.NewMigrator(x)
	migrator.LogLevel = log.INFO
	addMigrations(migrator)

	if err := migrator.Start(); err != nil {
		return fmt.Errorf("Sqlstore::Migration failed err: %v\n", err)
	}

	if err := x.Sync2(tables...); err != nil {
		return fmt.Errorf("sync database struct error: %v\n", err)
	}

	if enableLog {
		logPath := path.Join(setting.LogRootPath, "xorm.log")
		os.MkdirAll(path.Dir(logPath), os.ModePerm)

		f, err := os.Create(logPath)
		if err != nil {
			return fmt.Errorf("sqlstore.init(fail to create xorm.log): %v", err)
		}
		x.Logger = xorm.NewSimpleLogger(f)

		x.ShowSQL = true
		x.ShowInfo = true
		x.ShowDebug = true
		x.ShowErr = true
		x.ShowWarn = true
	}

	return nil
}

func getEngine() (*xorm.Engine, error) {
	LoadConfig()

	cnnstr := ""
	switch DbCfg.Type {
	case "mysql":
		cnnstr = fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8",
			DbCfg.User, DbCfg.Pwd, DbCfg.Host, DbCfg.Name)
	case "postgres":
		var host, port = "127.0.0.1", "5432"
		fields := strings.Split(DbCfg.Host, ":")
		if len(fields) > 0 && len(strings.TrimSpace(fields[0])) > 0 {
			host = fields[0]
		}
		if len(fields) > 1 && len(strings.TrimSpace(fields[1])) > 0 {
			port = fields[1]
		}
		cnnstr = fmt.Sprintf("user=%s password=%s host=%s port=%s dbname=%s sslmode=%s",
			DbCfg.User, DbCfg.Pwd, host, port, DbCfg.Name, DbCfg.SslMode)
	case "sqlite3":
		os.MkdirAll(path.Dir(DbCfg.Path), os.ModePerm)
		cnnstr = "file:" + DbCfg.Path + "?cache=shared&mode=rwc"
	default:
		return nil, fmt.Errorf("Unknown database type: %s", DbCfg.Type)
	}

	log.Info("Database: %v, ConnectionString: %v", DbCfg.Type, cnnstr)

	return xorm.NewEngine(DbCfg.Type, cnnstr)
}

func LoadConfig() {
	sec := setting.Cfg.Section("database")

	DbCfg.Type = sec.Key("type").String()
	if DbCfg.Type == "sqlite3" {
		UseSQLite3 = true
	}
	DbCfg.Host = sec.Key("host").String()
	DbCfg.Name = sec.Key("name").String()
	DbCfg.User = sec.Key("user").String()
	if len(DbCfg.Pwd) == 0 {
		DbCfg.Pwd = sec.Key("password").String()
	}
	DbCfg.SslMode = sec.Key("ssl_mode").String()
	DbCfg.Path = sec.Key("path").MustString("data/grafana.db")
}

type dbTransactionFunc func(sess *xorm.Session) error

func inTransaction(callback dbTransactionFunc) error {
	var err error

	sess := x.NewSession()
	defer sess.Close()

	if err = sess.Begin(); err != nil {
		return err
	}

	err = callback(sess)

	if err != nil {
		sess.Rollback()
		return err
	} else if err = sess.Commit(); err != nil {
		return err
	}

	return nil
}
