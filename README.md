# GoRexPro

Go client for [RexPro](https://github.com/tinkerpop/rexster/wiki/RexPro)/[Rexster](http://rexster.tinkerpop.com) 
with connection pooling as well as support for sessions. Compatable with [RexPro](https://github.com/tinkerpop/rexster/wiki/RexPro)
protocol version `1` ([Rexster](http://rexster.tinkerpop.com) v2.4.0 and above at the time of this writing).

## Usage
```go
import "github.com/philipsoutham/gorexpro/rexpro"
```

#### Simple
```go
func main() {
    var properties = map[string]interface{}{"properties": map[string]interface{}{"foo": "bar", "score": 5}}
    if c, err := rexpro.Dial("your-host:8184", "your-graph-name"); err == nil {
        defer c.Close()
        if resp, err := c.DoScript("g.addVertex(properties)", properties); err == nil {
            doSomethingWith(resp)
        }
    }
}

```

#### Connection Pools

```go
var RexproPool *rexpro.Pool

func init() {
    RexproPool = &rexpro.Pool{
		    MaxIdle:     5,
		    MaxActive:   200,
		    IdleTimeout: 25 * time.Second,
		    Dial: func() (rexpro.Conn, error) {
			    return rexpro.Dial("your-host:8184", "your-graph-name")
		    },
		    TestOnBorrow: func(c rexpro.Conn, t time.Time) error {
            _, err := c.DoScript("1", map[string]interface{}{})
			      return err
		    },
    }
}

func main() {
    var (
        c = RexproPool.Get()
        properties = map[string]interface{}{"properties": map[string]interface{}{"foo": "bar", "score": 5}}
    )
    defer c.Close()
    if resp, err := c.DoScript("g.addVertex(properties)", properties); err == nil {
        doSomethingWith(resp)
    }
}




```


## Unit Tests

Polishing them now, so coming soon (~~hopefully~~ er, maybe)


## Things you should probably be aware of...
Right now, some assumptions are made in regards to the `isolate` parameter in the [script request `Meta` map](https://github.com/tinkerpop/rexster/wiki/RexPro-Messages#session); 
that is to say that if you are in a "session", `isolate` will be set to `false` and `inSession` will be set to `true`. 
If you're not in a "session" the inverse will be applied. Also, the `graphObjName` parameter is always hardcoded to `"g"`.


## Thanks
to [@garyburd](https://github.com/garyburd) as I have copied much of the API he created for his excellent [redigo](https://github.com/garyburd/redigo) 
redis client and will be also leveraging his hard work for the unit tests as well.
