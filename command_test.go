package main

import (
	// "github.com/kylelemons/godebug/pretty"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	. "gopkg.in/check.v1"
	"sort"
	"time"
)

func (s MySuite) TestTranslateOpentsdb(c *C) {
	now := time.Now().Unix()
	ot := fmt.Sprintf("a.a %d 9 l1=v1\na.b %d 99 l2=v2 l3=v3", now, now+1)
	pms, err := translateOpenTsdb(ot)
	c.Assert(err, IsNil)
	c.Assert(len(pms), Equals, 2)

	c.Check(pms[0].Desc().String(), Equals, `Desc{fqName: "a_a", help: "help", constLabels: {l1="v1"}, variableLabels: []}`)

	var met1 dto.Metric
	pms[0].Write(&met1)
	// c.Check(met1.GetTimestampMs(), Equals, 0)
	g1 := met1.GetGauge()
	c.Assert(g1, Not(IsNil))
	c.Check(g1.GetValue(), Equals, 9.0)
	sort.Sort(prometheus.LabelPairSorter(met1.Label))
	c.Assert(len(met1.Label), Equals, 1)
	c.Check(met1.Label[0].GetName(), Equals, "l1")
	c.Check(met1.Label[0].GetValue(), Equals, "v1")

	c.Check(pms[1].Desc().String(), Equals, `Desc{fqName: "a_b", help: "help", constLabels: {l2="v2",l3="v3"}, variableLabels: []}`)

	var met2 dto.Metric
	pms[1].Write(&met2)
	// c.Check(met2.GetTimestampMs(), Equals, 0)
	g2 := met2.GetGauge()
	c.Assert(g2, Not(IsNil))
	c.Check(g2.GetValue(), Equals, 99.0)
	sort.Sort(prometheus.LabelPairSorter(met2.Label))
	c.Assert(len(met2.Label), Equals, 2)
	c.Check(met2.Label[0].GetName(), Equals, "l2")
	c.Check(met2.Label[0].GetValue(), Equals, "v2")
	c.Check(met2.Label[1].GetName(), Equals, "l3")
	c.Check(met2.Label[1].GetValue(), Equals, "v3")
}
