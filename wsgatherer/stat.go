package wsgatherer

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gomodule/redigo/redis"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
)

var queueDict = map[string]string{
	"heatmap": "heatmap_stats",
	"default": "realtime_stats",
}

func (s *Server) statHandler() httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		conn, err := wsUpgrade(w, r)
		if err != nil {
			fmt.Println(err)
			return
		}

		go statReader(conn, params.ByName("jwt"), s.Db.Get())
	}
}

func statReader(ws *websocket.Conn, jwtoken string, conn redis.Conn) {

	for {
		var msg map[string]string

		if err := ws.ReadJSON(&msg); err != nil {
			fmt.Println(err)
			break
		}

		fmt.Println("Received from client: ", msg["id"])

		if data, err := combineData(jwtoken, msg); err == nil {
			storeData(conn, data)
		} else {
			// close connection
			ws.Close()
		}
	}
	conn.Close()
}

func combineData(jwtoken string, input map[string]string) (map[string]string, error) {
	var data map[string]string

	jwtJSON, err := parseJWT(jwtoken)
	if err != nil {
		fmt.Println("Token parsing error: ", err)
		return input, err
	}

	if err = json.Unmarshal(jwtJSON, &data); err != nil {
		fmt.Println("JSON unmarshalling error: ", err)
		return input, err
	}

	fmt.Println(data)

	for k, v := range data {
		input[k] = v
	}

	return input, err
}

func storeData(conn redis.Conn, input map[string]string) {
	var queue string

	if event, ok := input["event"]; ok {
		queue = queueDict[event]
	} else {
		fmt.Println("Type assertion failed: event is not a string", event)
	}

	if queue == "" {
		queue = queueDict["default"]
	}

	msg, err := json.Marshal(input)

	if err != nil {
		fmt.Println("JSON marshalling error: ", err)
		return
	}

	if _, err := conn.Do("LPUSH", queue, string(msg)); err != nil {
		fmt.Println("Could not write to redis: ", err)
		return
	}

	// Read for debug
	res, err := redis.String(conn.Do("LPOP", queue))
	if err != nil {
		fmt.Println("Could not read from redis", err)
		return
	}
	fmt.Println("Read from redis: ", res)
}
