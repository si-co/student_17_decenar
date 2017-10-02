package service

/*
The service.go defines what to do for each API-call. This part of the service
runs on the node.
*/

import (
	"time"

	"errors"
	"net/http"
	"sync"

	"github.com/nblp/decenarch"
	"github.com/nblp/decenarch/protocol"

	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/network"

	"golang.org/x/crypto/bcrypt"
)

// Used for tests
var templateID onet.ServiceID

func init() {
	var err error
	templateID, err = onet.RegisterNewService(template.ServiceName, newService)
	log.ErrFatal(err)
	network.RegisterMessage(&storage{})
}

// Service is our template-service
type Service struct {
	// We need to embed the ServiceProcessor, so that incoming messages
	// are correctly handled.
	*onet.ServiceProcessor

	storage *storage
}

// storageID reflects the data we're storing - we could store more
// than one structure.
const storageID = "main"

// storage is used to save our data.
type storage struct {
	Count      int
	webarchive map[string]webstore

	sync.Mutex
}

// webstore is used to store website
type webstore struct {
	Hash []byte
	Url  string
}

// SaveRequest
func (s *Service) SaveRequest(req *template.SaveRequest) (*template.SaveResponse, onet.ClientError) {
	// start save protocol
	tree := req.Roster.GenerateNaryTreeWithRoot(2, s.ServerIdentity())
	if tree == nil {
		return nil, onet.NewClientErrorCode(template.ErrorParse, "couldn't create tree")
	}
	//TODO pass url to conode
	//pi, err := s.CreateProtocol(protocol.Name, tree)
	//if err != nill {
	//	return nil, ont.NewClientErrorCode(err)
	//}
	pi, err := service.NewProtocol(tree)
	pi.Url = req.Url
	if err != nil {
		return nil, onet.NewClientErrorCode(err)
	}

	pi.Start()
	resp := &template.SaveResponse{}
	// record website in saved website index
	url := req.Url
	hash := GenerateFromPassword([]byte(url), 30)
	web := webstore{
		Hash: hash,
		Url:  url,
	}
	s.storage.Lock()
	s.storage.webarchive[url] = web
	s.storage.Unlock()
	s.save()
	return resp, nil
}

// RetrieveRequest
func (s *Service) RetriveRequest(req *template.RetrieveRequest) (*template.RetrieveResponse, onet.ClientError) {
	if web, isSaved := s.webarchive[req.Url]; isSaved {
		web.Lock()
		defer web.Unlock()
		//TODO need to send File or []byte + all needed data
		return &template.RetrieveResponse{Website: web.IndexHtmlPath}, nil
	} else {
		return nil, onet.NewClientErrorCode(template.ErrorParse, "website requested was not saved")
	}
}

// ClockRequest starts a template-protocol and returns the run-time.
func (s *Service) ClockRequest(req *template.ClockRequest) (*template.ClockResponse, onet.ClientError) {
	s.storage.Lock()
	s.storage.Count++
	s.storage.Unlock()
	s.save()
	tree := req.Roster.GenerateNaryTreeWithRoot(2, s.ServerIdentity())
	if tree == nil {
		return nil, onet.NewClientErrorCode(template.ErrorParse, "couldn't create tree")
	}
	pi, err := s.CreateProtocol(protocol.Name, tree)
	if err != nil {
		return nil, onet.NewClientError(err)
	}
	start := time.Now()
	pi.Start()
	resp := &template.ClockResponse{
		Children: <-pi.(*protocol.Template).ChildCount,
	}
	resp.Time = time.Now().Sub(start).Seconds()
	return resp, nil
}

// CountRequest returns the number of instantiations of the protocol.
func (s *Service) CountRequest(req *template.CountRequest) (*template.CountResponse, onet.ClientError) {
	s.storage.Lock()
	defer s.storage.Unlock()
	return &template.CountResponse{Count: s.storage.Count}, nil
}

// NewProtocol is called on all nodes of a Tree (except the root, since it is
// the one starting the protocol) so it's the Service that will be called to
// generate the PI on all others node.
// If you use CreateProtocolOnet, this will not be called, as the Onet will
// instantiate the protocol on its own. If you need more control at the
// instantiation of the protocol, use CreateProtocolService, and you can
// give some extra-configuration to your protocol in here.
func (s *Service) NewProtocol(tn *onet.TreeNodeInstance, conf *onet.GenericConfig) (onet.ProtocolInstance, error) {
	log.Lvl3("Decenarch Service new protocol event")
	pi, err := protocol.NewProtocol(tn)
	if err != nil {
		return nil, err
	}

	s.RegisterProtocolInstance(pi)
	return pi, err
}

// saves all skipblocks.
func (s *Service) save() {
	s.storage.Lock()
	defer s.storage.Unlock()
	err := s.Save(storageID, s.storage)
	if err != nil {
		log.Error("Couldn't save file:", err)
	}
}

// Tries to load the configuration and updates the data in the service
// if it finds a valid config-file.
func (s *Service) tryLoad() error {
	s.storage = &storage{}
	if !s.DataAvailable(storageID) {
		return nil
	}
	msg, err := s.Load(storageID)
	if err != nil {
		return err
	}
	var ok bool
	s.storage, ok = msg.(*storage)
	if !ok {
		return errors.New("Data of wrong type")
	}
	return nil
}

// newService receives the context that holds information about the node it's
// running on. Saving and loading can be done using the context. The data will
// be stored in memory for tests and simulations, and on disk for real deployments.
func newService(c *onet.Context) onet.Service {
	s := &Service{
		ServiceProcessor: onet.NewServiceProcessor(c),
	}
	if err := s.RegisterHandlers(s.ClockRequest, s.CountRequest); err != nil {
		log.ErrFatal(err, "Couldn't register messages")
	}
	if err := s.tryLoad(); err != nil {
		log.Error(err)
	}
	return s
}
