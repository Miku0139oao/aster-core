package outbound

import "github.com/Miku0139oao/aster-core/transport/jls"

type JLSOptions struct {
	Username string `proxy:"username"`
	Password string `proxy:"password"`
}

func (o JLSOptions) Parse() (*jls.Config, error) {
	if o.Username == "" && o.Password == "" {
		return nil, nil
	}
	return jls.NewConfig(o.Username, o.Password)
}
