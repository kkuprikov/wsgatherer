// Package wsgatherer - this files provides API for saving statistics data
package wsgatherer

import (
	"context"
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

func (s *Server) statHandler(ctx context.Context) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		ws, err := wsUpgrade(w, r)
		if err != nil {
			fmt.Println(err)
			return
		}

		reqCtx, cancel := context.WithCancel(r.Context())
		r = r.WithContext(reqCtx)

		statReader(ctx, r.Context(), cancel, ws, params.ByName("jwt"), s.Db)
	}
}

func statReader(ctx context.Context, reqCtx context.Context, cancel func(), ws *websocket.Conn, jwtoken string, pool *redis.Pool) {
	for {
	FirstSelect:
		select {
		//gracefully close connection on ctx.Done() or reqCtx.Done()
		case <-reqCtx.Done():
			fmt.Println("reqCtx.Done() in statReader")
			Check(ws.Close)
			return
		case <-ctx.Done():
			fmt.Println("ctx.Done() in statReader")
			Check(ws.Close)
			return
		default:
			var msg map[string]string

			if err := ws.ReadJSON(&msg); err != nil {
				switch err.(type) {
				case *websocket.CloseError:
					cancel()
					break FirstSelect
				default:
					fmt.Println("Error while reading JSON from client: ", err)
					return
				}
			}

			fmt.Println("Received from client: ", msg["id"])
			data, err := combineData(jwtoken, msg)

			if err != nil {
				fmt.Println("Error when combining data: ", err)
				return
			}
			storeData(pool, data)
		}
	}
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

func storeData(pool *redis.Pool, input map[string]string) {
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

	err = sendAndClose(pool, "LPUSH", queue, string(msg))

	if err != nil {
		fmt.Println("Could not write to redis: ", err)
		return
	}

	// Read for debug
	res, err := redis.String(doAndClose(pool, "LPOP", queue))

	if err != nil {
		fmt.Println("Could not read from redis", err)
		return
	}

	fmt.Println("Read from redis: ", res)
}
