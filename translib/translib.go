////////////////////////////////////////////////////////////////////////////////
//                                                                            //
//  Copyright 2019 Broadcom. The term Broadcom refers to Broadcom Inc. and/or //
//  its subsidiaries.                                                         //
//                                                                            //
//  Licensed under the Apache License, Version 2.0 (the "License");           //
//  you may not use this file except in compliance with the License.          //
//  You may obtain a copy of the License at                                   //
//                                                                            //
//     http://www.apache.org/licenses/LICENSE-2.0                             //
//                                                                            //
//  Unless required by applicable law or agreed to in writing, software       //
//  distributed under the License is distributed on an "AS IS" BASIS,         //
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  //  
//  See the License for the specific language governing permissions and       //
//  limitations under the License.                                            //
//                                                                            //
////////////////////////////////////////////////////////////////////////////////

/*
Package translib implements APIs like Create, Get, Subscribe etc.

to be consumed by the north bound management server implementations

This package takes care of translating the incoming requests to

Redis ABNF format and persisting them in the Redis DB.

It can also translate the ABNF format to YANG specific JSON IETF format

This package can also talk to non-DB clients.
*/

package translib

import (
	"sync"
	"github.com/Azure/sonic-mgmt-common/translib/db"
	"github.com/Azure/sonic-mgmt-common/translib/tlerr"
	"github.com/Workiva/go-datastructures/queue"
	log "github.com/golang/glog"
)

//Write lock for all write operations to be synchronized
var writeMutex = &sync.Mutex{}

//Interval value for interval based subscription needs to be within the min and max
//minimum global interval for interval based subscribe in secs
var minSubsInterval = 20
//maximum global interval for interval based subscribe in secs
var maxSubsInterval = 600

type ErrSource int

const (
	ProtoErr ErrSource = iota
	AppErr
)

type UserRoles struct {
	Name    string
	Roles	[]string
}

type SetRequest struct {
	Path    string
	Payload []byte
	User    UserRoles
	AuthEnabled bool
	ClientVersion Version
	DeleteEmptyEntry bool
}

type SetResponse struct {
	ErrSrc ErrSource
	Err    error
}

type GetRequest struct {
	Path    string
	User    UserRoles
	AuthEnabled bool
	ClientVersion Version

	// Depth limits the depth of data subtree in the response
	// payload. Default value 0 indicates there is no limit.
	Depth   uint
}

type GetResponse struct {
	Payload []byte
	ErrSrc  ErrSource
}

type ActionRequest struct {
	Path    string
	Payload []byte
	User    UserRoles
	AuthEnabled bool
	ClientVersion Version
}

type ActionResponse struct {
	Payload []byte
	ErrSrc  ErrSource
}

type BulkRequest struct {
	DeleteRequest  []SetRequest
	ReplaceRequest []SetRequest
	UpdateRequest  []SetRequest
	CreateRequest  []SetRequest
	User           UserRoles
	AuthEnabled    bool
	ClientVersion  Version
}

type BulkResponse struct {
	DeleteResponse  []SetResponse
	ReplaceResponse []SetResponse
	UpdateResponse  []SetResponse
	CreateResponse  []SetResponse
}

type SubscribeRequest struct {
	Paths			[]string
	Q				*queue.PriorityQueue
	Stop			chan struct{}
	User            UserRoles
	AuthEnabled     bool
	ClientVersion   Version
}

type SubscribeResponse struct {
	Path         string
	Payload      []byte
	Timestamp    int64
	SyncComplete bool
	IsTerminated bool
}

type NotificationType int

const (
	Sample NotificationType = iota
	OnChange
)

type IsSubscribeRequest struct {
	Paths				[]string
	User                UserRoles
	AuthEnabled         bool
	ClientVersion       Version
}

type IsSubscribeResponse struct {
	Path                string
	IsOnChangeSupported bool
	MinInterval         int
	Err                 error
	PreferredType       NotificationType
}

type ModelData struct {
	Name string
	Org  string
	Ver  string
}

type notificationOpts struct {
    isOnChangeSupported bool
	mInterval           int
	pType               NotificationType // for TARGET_DEFINED
}

//initializes logging and app modules
func init() {
	log.Flush()
}

//Create - Creates entries in the redis DB pertaining to the path and payload
func Create(req SetRequest) (SetResponse, error) {
	var keys []db.WatchKeys
	var resp SetResponse
	path := req.Path
	payload := req.Payload
	if !isAuthorizedForSet(req) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Create Operation",
			Path: path,
		}
	}

	log.Info("Create request received with path =", path)
	log.Info("Create request received with payload =", string(payload))

	app, appInfo, err := getAppModule(path, req.ClientVersion)

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	err = appInitialize(app, appInfo, path, &payload, nil, CREATE)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	writeMutex.Lock()
	defer writeMutex.Unlock()

	isWriteDisabled := false
	d, err := db.NewDB(getDBOptions(db.ConfigDB, isWriteDisabled))

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	defer d.DeleteDB()

	keys, err = (*app).translateCreate(d)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.StartTx(keys, appInfo.tablesToWatch)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	resp, err = (*app).processCreate(d)

	if err != nil {
		d.AbortTx()
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.CommitTx()

	if err != nil {
		resp.ErrSrc = AppErr
	}

	return resp, err
}

//Update - Updates entries in the redis DB pertaining to the path and payload
func Update(req SetRequest) (SetResponse, error) {
	var keys []db.WatchKeys
	var resp SetResponse
	path := req.Path
	payload := req.Payload
	if !isAuthorizedForSet(req) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Update Operation",
			Path: path,
		}
	}


	log.Info("Update request received with path =", path)
	log.Info("Update request received with payload =", string(payload))

	app, appInfo, err := getAppModule(path, req.ClientVersion)

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	err = appInitialize(app, appInfo, path, &payload, nil, UPDATE)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	writeMutex.Lock()
	defer writeMutex.Unlock()

	isWriteDisabled := false
	d, err := db.NewDB(getDBOptions(db.ConfigDB, isWriteDisabled))

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	defer d.DeleteDB()

	keys, err = (*app).translateUpdate(d)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.StartTx(keys, appInfo.tablesToWatch)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	resp, err = (*app).processUpdate(d)

	if err != nil {
		d.AbortTx()
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.CommitTx()

	if err != nil {
		resp.ErrSrc = AppErr
	}

	return resp, err
}

//Replace - Replaces entries in the redis DB pertaining to the path and payload
func Replace(req SetRequest) (SetResponse, error) {
	var err error
	var keys []db.WatchKeys
	var resp SetResponse
	path := req.Path
	payload := req.Payload
	if !isAuthorizedForSet(req) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Replace Operation",
			Path: path,
		}
	}

	log.Info("Replace request received with path =", path)
	log.Info("Replace request received with payload =", string(payload))

	app, appInfo, err := getAppModule(path, req.ClientVersion)

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	err = appInitialize(app, appInfo, path, &payload, nil, REPLACE)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	writeMutex.Lock()
	defer writeMutex.Unlock()

	isWriteDisabled := false
	d, err := db.NewDB(getDBOptions(db.ConfigDB, isWriteDisabled))

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	defer d.DeleteDB()

	keys, err = (*app).translateReplace(d)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.StartTx(keys, appInfo.tablesToWatch)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	resp, err = (*app).processReplace(d)

	if err != nil {
		d.AbortTx()
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.CommitTx()

	if err != nil {
		resp.ErrSrc = AppErr
	}

	return resp, err
}

//Delete - Deletes entries in the redis DB pertaining to the path
func Delete(req SetRequest) (SetResponse, error) {
	var err error
	var keys []db.WatchKeys
	var resp SetResponse
	path := req.Path
	if !isAuthorizedForSet(req) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Delete Operation",
			Path: path,
		}
	}

	log.Info("Delete request received with path =", path)

	app, appInfo, err := getAppModule(path, req.ClientVersion)

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	opts := appOptions{deleteEmptyEntry: req.DeleteEmptyEntry}
	err = appInitialize(app, appInfo, path, nil, &opts, DELETE)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	writeMutex.Lock()
	defer writeMutex.Unlock()

	isWriteDisabled := false
	d, err := db.NewDB(getDBOptions(db.ConfigDB, isWriteDisabled))

	if err != nil {
		resp.ErrSrc = ProtoErr
		return resp, err
	}

	defer d.DeleteDB()

	keys, err = (*app).translateDelete(d)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.StartTx(keys, appInfo.tablesToWatch)

	if err != nil {
		resp.ErrSrc = AppErr
		return resp, err
	}

	resp, err = (*app).processDelete(d)

	if err != nil {
		d.AbortTx()
		resp.ErrSrc = AppErr
		return resp, err
	}

	err = d.CommitTx()

	if err != nil {
		resp.ErrSrc = AppErr
	}

	return resp, err
}

//Get - Gets data from the redis DB and converts it to northbound format
func Get(req GetRequest) (GetResponse, error) {
	var payload []byte
	var resp GetResponse
	path := req.Path
	if !isAuthorizedForGet(req) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Get Operation",
			Path: path,
		}
	}

	log.Info("Received Get request for path = ", path)

	app, appInfo, err := getAppModule(path, req.ClientVersion)

	if err != nil {
		resp = GetResponse{Payload: payload, ErrSrc: ProtoErr}
		return resp, err
	}

	opts := appOptions{ depth: req.Depth }
	err = appInitialize(app, appInfo, path, nil, &opts, GET)

	if err != nil {
		resp = GetResponse{Payload: payload, ErrSrc: AppErr}
		return resp, err
	}

	isGetCase := true
	dbs, err := getAllDbs(isGetCase)

	if err != nil {
		resp = GetResponse{Payload: payload, ErrSrc: ProtoErr}
		return resp, err
	}

	defer closeAllDbs(dbs[:])

	err = (*app).translateGet(dbs)

	if err != nil {
		resp = GetResponse{Payload: payload, ErrSrc: AppErr}
		return resp, err
	}

	resp, err = (*app).processGet(dbs)

	return resp, err
}

func Action(req ActionRequest) (ActionResponse, error) {
	var payload []byte
	var resp ActionResponse
	path := req.Path

	if !isAuthorizedForAction(req) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Action Operation",
			Path: path,
		}
	}

	log.Info("Received Action request for path = ", path)

	app, appInfo, err := getAppModule(path, req.ClientVersion)

	if err != nil {
		resp = ActionResponse{Payload: payload, ErrSrc: ProtoErr}
		return resp, err
	}

	aInfo := *appInfo

	aInfo.isNative = true

	err = appInitialize(app, &aInfo, path, &req.Payload, nil, GET)

	if err != nil {
		resp = ActionResponse{Payload: payload, ErrSrc: AppErr}
		return resp, err
	}

    writeMutex.Lock()
    defer writeMutex.Unlock()

	isGetCase := false
	dbs, err := getAllDbs(isGetCase)

	if err != nil {
		resp = ActionResponse{Payload: payload, ErrSrc: ProtoErr}
		return resp, err
	}

	defer closeAllDbs(dbs[:])

	err = (*app).translateAction(dbs)

	if err != nil {
		resp = ActionResponse{Payload: payload, ErrSrc: AppErr}
		return resp, err
	}

	resp, err = (*app).processAction(dbs)

	return resp, err
}

func Bulk(req BulkRequest) (BulkResponse, error) {
	var err error
	var keys []db.WatchKeys
	var errSrc ErrSource

	delResp := make([]SetResponse, len(req.DeleteRequest))
	replaceResp := make([]SetResponse, len(req.ReplaceRequest))
	updateResp := make([]SetResponse, len(req.UpdateRequest))
	createResp := make([]SetResponse, len(req.CreateRequest))

	resp := BulkResponse{DeleteResponse: delResp,
		ReplaceResponse: replaceResp,
		UpdateResponse: updateResp,
		CreateResponse: createResp}


    if (!isAuthorizedForBulk(req)) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Action Operation",
		}
    }

	writeMutex.Lock()
	defer writeMutex.Unlock()

	isWriteDisabled := false
	d, err := db.NewDB(getDBOptions(db.ConfigDB, isWriteDisabled))

	if err != nil {
		return resp, err
	}

	defer d.DeleteDB()

	//Start the transaction without any keys or tables to watch will be added later using AppendWatchTx
	err = d.StartTx(nil, nil)

	if err != nil {
        return resp, err
    }

	for i := range req.DeleteRequest {
		path := req.DeleteRequest[i].Path
		opts := appOptions{deleteEmptyEntry: req.DeleteRequest[i].DeleteEmptyEntry}

		log.Info("Delete request received with path =", path)

		app, appInfo, err := getAppModule(path, req.DeleteRequest[i].ClientVersion)

		if err != nil {
			errSrc = ProtoErr
			goto BulkDeleteError
		}

		err = appInitialize(app, appInfo, path, nil, &opts, DELETE)

		if err != nil {
			errSrc = AppErr
			goto BulkDeleteError
		}

		keys, err = (*app).translateDelete(d)

		if err != nil {
			errSrc = AppErr
			goto BulkDeleteError
		}

		err = d.AppendWatchTx(keys, appInfo.tablesToWatch)

		if err != nil {
			errSrc = AppErr
			goto BulkDeleteError
		}

		resp.DeleteResponse[i], err = (*app).processDelete(d)

		if err != nil {
			errSrc = AppErr
		}

	BulkDeleteError:

		if err != nil {
			d.AbortTx()
			resp.DeleteResponse[i].ErrSrc = errSrc
			resp.DeleteResponse[i].Err = err
			return resp, err
		}
	}

    for i := range req.ReplaceRequest {
        path := req.ReplaceRequest[i].Path
		payload := req.ReplaceRequest[i].Payload

        log.Info("Replace request received with path =", path)

        app, appInfo, err := getAppModule(path, req.ReplaceRequest[i].ClientVersion)

        if err != nil {
            errSrc = ProtoErr
            goto BulkReplaceError
        }

		log.Info("Bulk replace request received with path =", path)
		log.Info("Bulk replace request received with payload =", string(payload))

		err = appInitialize(app, appInfo, path, &payload, nil, REPLACE)

        if err != nil {
            errSrc = AppErr
            goto BulkReplaceError
        }

        keys, err = (*app).translateReplace(d)

        if err != nil {
            errSrc = AppErr
            goto BulkReplaceError
        }

        err = d.AppendWatchTx(keys, appInfo.tablesToWatch)

        if err != nil {
            errSrc = AppErr
            goto BulkReplaceError
        }

        resp.ReplaceResponse[i], err = (*app).processReplace(d)

        if err != nil {
            errSrc = AppErr
        }

    BulkReplaceError:

        if err != nil {
            d.AbortTx()
            resp.ReplaceResponse[i].ErrSrc = errSrc
            resp.ReplaceResponse[i].Err = err
            return resp, err
        }
    }

	for i := range req.UpdateRequest {
		path := req.UpdateRequest[i].Path
		payload := req.UpdateRequest[i].Payload

		log.Info("Update request received with path =", path)

		app, appInfo, err := getAppModule(path, req.UpdateRequest[i].ClientVersion)

		if err != nil {
			errSrc = ProtoErr
			goto BulkUpdateError
		}

		err = appInitialize(app, appInfo, path, &payload, nil, UPDATE)

		if err != nil {
			errSrc = AppErr
			goto BulkUpdateError
		}

		keys, err = (*app).translateUpdate(d)

		if err != nil {
			errSrc = AppErr
			goto BulkUpdateError
		}

		err = d.AppendWatchTx(keys, appInfo.tablesToWatch)

		if err != nil {
			errSrc = AppErr
			goto BulkUpdateError
		}

		resp.UpdateResponse[i], err = (*app).processUpdate(d)

		if err != nil {
			errSrc = AppErr
		}

	BulkUpdateError:

		if err != nil {
			d.AbortTx()
			resp.UpdateResponse[i].ErrSrc = errSrc
			resp.UpdateResponse[i].Err = err
			return resp, err
		}
	}

	for i := range req.CreateRequest {
		path := req.CreateRequest[i].Path
		payload := req.CreateRequest[i].Payload

		log.Info("Create request received with path =", path)

		app, appInfo, err := getAppModule(path, req.CreateRequest[i].ClientVersion)

		if err != nil {
			errSrc = ProtoErr
			goto BulkCreateError
		}

		err = appInitialize(app, appInfo, path, &payload, nil, CREATE)

		if err != nil {
			errSrc = AppErr
			goto BulkCreateError
		}

		keys, err = (*app).translateCreate(d)

		if err != nil {
			errSrc = AppErr
			goto BulkCreateError
		}

		err = d.AppendWatchTx(keys, appInfo.tablesToWatch)

		if err != nil {
			errSrc = AppErr
			goto BulkCreateError
		}

		resp.CreateResponse[i], err = (*app).processCreate(d)

		if err != nil {
			errSrc = AppErr
		}

	BulkCreateError:

		if err != nil {
			d.AbortTx()
			resp.CreateResponse[i].ErrSrc = errSrc
			resp.CreateResponse[i].Err = err
			return resp, err
		}
	}

	err = d.CommitTx()

	return resp, err
}

//Subscribe - Subscribes to the paths requested and sends notifications when the data changes in DB
func Subscribe(req SubscribeRequest) ([]*IsSubscribeResponse, error) {
	var err error
	var sErr error

	paths := req.Paths
	q     := req.Q
	stop  := req.Stop

	dbNotificationMap := make(map[db.DBNum][]*notificationInfo)

	resp := make([]*IsSubscribeResponse, len(paths))

	for i := range resp {
		resp[i] = &IsSubscribeResponse{Path: paths[i],
			IsOnChangeSupported: false,
			MinInterval:         minSubsInterval,
			PreferredType:       Sample,
			Err:                 nil}
	}

    if (!isAuthorizedForSubscribe(req)) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Action Operation",
		}
    }

	isGetCase := true
	dbs, err := getAllDbs(isGetCase)

	if err != nil {
		return resp, err
	}

	//Do NOT close the DBs here as we need to use them during subscribe notification

	for i, path := range paths {

		app, appInfo, err := getAppModule(path, req.ClientVersion)

		if err != nil {

			if sErr == nil {
				sErr = err
			}

			resp[i].Err = err
			continue
		}

		nOpts, nInfo, errApp := (*app).translateSubscribe(dbs, path)

		if nOpts != nil {
			if nOpts.mInterval != 0 {
				if ((nOpts.mInterval >= minSubsInterval) && (nOpts.mInterval <= maxSubsInterval)) {
					resp[i].MinInterval = nOpts.mInterval
				} else if (nOpts.mInterval < minSubsInterval) {
					resp[i].MinInterval = minSubsInterval
				} else {
					resp[i].MinInterval = maxSubsInterval
				}
			}

			resp[i].IsOnChangeSupported = nOpts.isOnChangeSupported
			resp[i].PreferredType = nOpts.pType
		}

		if errApp != nil {
			resp[i].Err = errApp

			if sErr == nil {
				sErr = errApp
			}

			continue
		} else {

			if nInfo == nil {
				sErr = tlerr.NotSupportedError{
					Format: "Subscribe not supported", Path: path}
				resp[i].Err = sErr
				continue
			}

			nInfo.path = path
			nInfo.app = app
			nInfo.appInfo = appInfo
			nInfo.dbs = dbs

			dbNotificationMap[nInfo.dbno] = append(dbNotificationMap[nInfo.dbno], nInfo)
		}

	}

	log.Info("map=", dbNotificationMap)

	if sErr != nil {
		return resp, sErr
	}

	sInfo := &subscribeInfo{syncDone: false,
		q:    q,
		stop: stop}

	sErr = startSubscribe(sInfo, dbNotificationMap)

	return resp, sErr
}

//IsSubscribeSupported - Check if subscribe is supported on the given paths
func IsSubscribeSupported(req IsSubscribeRequest) ([]*IsSubscribeResponse, error) {

	paths := req.Paths
	resp := make([]*IsSubscribeResponse, len(paths))

	for i := range resp {
		resp[i] = &IsSubscribeResponse{Path: paths[i],
			IsOnChangeSupported: false,
			MinInterval:         minSubsInterval,
			PreferredType:       Sample,
			Err:                 nil}
	}

    if (!isAuthorizedForIsSubscribe(req)) {
		return resp, tlerr.AuthorizationError{
			Format: "User is unauthorized for Action Operation",
		}
    }

	isGetCase := true
	dbs, err := getAllDbs(isGetCase)

	if err != nil {
		return resp, err
	}

	defer closeAllDbs(dbs[:])

	for i, path := range paths {

		app, _, err := getAppModule(path, req.ClientVersion)

		if err != nil {
			resp[i].Err = err
			continue
		}

		nOpts, _, errApp := (*app).translateSubscribe(dbs, path)

        if nOpts != nil {
            if nOpts.mInterval != 0 {
                if ((nOpts.mInterval >= minSubsInterval) && (nOpts.mInterval <= maxSubsInterval)) {
                    resp[i].MinInterval = nOpts.mInterval
                } else if (nOpts.mInterval < minSubsInterval) {
                    resp[i].MinInterval = minSubsInterval
                } else {
                    resp[i].MinInterval = maxSubsInterval
                }
            }

            resp[i].IsOnChangeSupported = nOpts.isOnChangeSupported
            resp[i].PreferredType = nOpts.pType
        }

		if errApp != nil {
			resp[i].Err = errApp
			err = errApp

			continue
		}
	}

	return resp, err
}

//GetModels - Gets all the models supported by Translib
func GetModels() ([]ModelData, error) {
	var err error

	return getModels(), err
}

//Creates connection will all the redis DBs. To be used for get request
func getAllDbs(isGetCase bool) ([db.MaxDB]*db.DB, error) {
	var dbs [db.MaxDB]*db.DB
	var err error
	var isWriteDisabled bool

	if isGetCase {
		isWriteDisabled = true
	} else {
		isWriteDisabled = false
	}

	//Create Application DB connection
	dbs[db.ApplDB], err = db.NewDB(getDBOptions(db.ApplDB, isWriteDisabled))

	if err != nil {
		closeAllDbs(dbs[:])
		return dbs, err
	}

	//Create ASIC DB connection
	dbs[db.AsicDB], err = db.NewDB(getDBOptions(db.AsicDB, isWriteDisabled))

	if err != nil {
		closeAllDbs(dbs[:])
		return dbs, err
	}

	//Create Counter DB connection
	dbs[db.CountersDB], err = db.NewDB(getDBOptions(db.CountersDB, isWriteDisabled))

	if err != nil {
		closeAllDbs(dbs[:])
		return dbs, err
	}

    isWriteDisabled = true 

	//Create Config DB connection
	dbs[db.ConfigDB], err = db.NewDB(getDBOptions(db.ConfigDB, isWriteDisabled))

	if err != nil {
		closeAllDbs(dbs[:])
		return dbs, err
	}

    if isGetCase {
        isWriteDisabled = true 
    } else {
        isWriteDisabled = false
    }

	//Create Flex Counter DB connection
	dbs[db.FlexCounterDB], err = db.NewDB(getDBOptions(db.FlexCounterDB, isWriteDisabled))

	if err != nil {
		closeAllDbs(dbs[:])
		return dbs, err
	}

	//Create State DB connection
	dbs[db.StateDB], err = db.NewDB(getDBOptions(db.StateDB, isWriteDisabled))

	if err != nil {
		closeAllDbs(dbs[:])
		return dbs, err
	}

	return dbs, err
}

//Closes the dbs, and nils out the arr.
func closeAllDbs(dbs []*db.DB) {
	for dbsi, d := range dbs {
		if d != nil {
			d.DeleteDB()
			dbs[dbsi] = nil
		}
	}
}

// Compare - Implement Compare method for priority queue for SubscribeResponse struct
func (val SubscribeResponse) Compare(other queue.Item) int {
	o := other.(*SubscribeResponse)
	if val.Timestamp > o.Timestamp {
		return 1
	} else if val.Timestamp == o.Timestamp {
		return 0
	}
	return -1
}

func getDBOptions(dbNo db.DBNum, isWriteDisabled bool) db.Options {
	var opt db.Options

	switch dbNo {
	case db.ApplDB, db.CountersDB, db.AsicDB:
		opt = getDBOptionsWithSeparator(dbNo, "", ":", ":", isWriteDisabled)
	case db.FlexCounterDB, db.ConfigDB, db.StateDB:
		opt = getDBOptionsWithSeparator(dbNo, "", "|", "|", isWriteDisabled)
	}

	return opt
}

func getDBOptionsWithSeparator(dbNo db.DBNum, initIndicator string, tableSeparator string, keySeparator string, isWriteDisabled bool) db.Options {
	return (db.Options{
		DBNo:               dbNo,
		InitIndicator:      initIndicator,
		TableNameSeparator: tableSeparator,
		KeySeparator:       keySeparator,
		IsWriteDisabled:    isWriteDisabled,
	})
}

func getAppModule(path string, clientVer Version) (*appInterface, *appInfo, error) {
	var app appInterface

	aInfo, err := getAppModuleInfo(path)

	if err != nil {
		return nil, aInfo, err
	}

	if err := validateClientVersion(clientVer, path, aInfo); err != nil {
		return nil, aInfo, err
	}

	app, err = getAppInterface(aInfo.appType)

	if err != nil {
		return nil, aInfo, err
	}

	return &app, aInfo, err
}

func appInitialize(app *appInterface, appInfo *appInfo, path string, payload *[]byte, opts *appOptions, opCode int) error {
	var err error
	var input []byte

	if payload != nil {
		input = *payload
	}

	if appInfo.isNative {
		log.Info("Native MSFT format")
		data := appData{path: path, payload: input}
		data.setOptions(opts)
		(*app).initialize(data)
	} else {
		ygotStruct, ygotTarget, err := getRequestBinder(&path, payload, opCode, &(appInfo.ygotRootType)).unMarshall()
		if err != nil {
			log.Info("Error in request binding: ", err)
			return err
		}

		data := appData{path: path, payload: input, ygotRoot: ygotStruct, ygotTarget: ygotTarget}
		data.setOptions(opts)
		(*app).initialize(data)
	}

	return err
}

func (data *appData) setOptions(opts *appOptions) {
	if opts != nil {
		data.appOptions = *opts
	}
}
