package timestamped

import "github.com/p2pquake/jmaxml-seis-parser-go/epsp"

type JMAQuakeWithTimestamp struct {
	epsp.JMAQuake
	Timestamp Timestamp `json:"timestamp"`
}

type JMATsunamiWithTimestamp struct {
	epsp.JMATsunami
	Timestamp Timestamp `json:"timestamp"`
}

type JMAEEWWithTimestamp struct {
	epsp.JMAEEW
	Timestamp Timestamp `json:"timestamp"`
}

type Timestamp struct {
	Convert  string `json:"convert"`
	Register string `json:"register"`
}
