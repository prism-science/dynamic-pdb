package db

type Config struct {
	Host                  string `mapstructure:"host"`
	Name                  string `mapstructure:"name"`
	Port                  int    `mapstructure:"port"`
	Username              string `mapstructure:"username"`
	Password              string `mapstructure:"password"`
	ConnectionParams      string `mapstructure:"connection_params"`
	MaxConnectionIdleTime string `mapstructure:"max_connection_idle_time"`
	MaxConnectionLifetime string `mapstructure:"max_connection_lifetime"`
	MaxOpenConnections    int    `mapstructure:"max_open_connections"`
	MaxIdleConnections    int    `mapstructure:"max_idle_connections"`
}
