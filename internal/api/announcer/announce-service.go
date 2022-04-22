package announcer

import (
	"context"
	"net"
	"net/url"
	"runtime"
	"sort"
	"sync"

	grpcRt "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/pkg/errors"
	"github.com/thataway/common-lib/pkg/slice"
	"github.com/thataway/common-lib/server"
	apiHelper "github.com/thataway/protos/pkg/api"
	"github.com/thataway/protos/pkg/api/announcer"
	"github.com/vishvananda/netlink"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

//"github.com/thataway/announcer/pkg/announcer"

//NewAnnounceService creates AnnounceService as server.APIService
func NewAnnounceService(ctx context.Context, netInterfaceName string) (server.APIService, error) {
	const api = "NewAnnounceService"

	netIface, err := net.InterfaceByName(netInterfaceName)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: net-intarface '%s'", api, netInterfaceName)
	}
	appCtx, stop := context.WithCancel(ctx)
	ret := &announceService{
		appCtx:       appCtx,
		stop:         stop,
		netInterface: netIface,
		sema:         make(chan struct{}, 1),
	}
	runtime.SetFinalizer(ret, func(o *announceService) {
		if o.sema != nil {
			close(o.sema)
		}
	})
	return ret, nil
}

var (
	_ announcer.AnnounceServiceServer = (*announceService)(nil)
	_ server.APIService               = (*announceService)(nil)
	_ server.APIGatewayProxy          = (*announceService)(nil)
	_ server.APIServiceOnStopEvent    = (*announceService)(nil)

	//GetSwaggerDocs get swagger spec docs
	GetSwaggerDocs = apiHelper.Announcer.LoadSwagger
)

const (
	mask32 = "/32"
)

type announceService struct {
	announcer.UnimplementedAnnounceServiceServer
	appCtx       context.Context
	stop         func()
	netInterface *net.Interface

	sema chan struct{}
}

//Description impl server.APIService
func (srv *announceService) Description() grpc.ServiceDesc {
	return announcer.AnnounceService_ServiceDesc
}

//RegisterGRPC impl server.APIService
func (srv *announceService) RegisterGRPC(_ context.Context, s *grpc.Server) error {
	announcer.RegisterAnnounceServiceServer(s, srv)
	return nil
}

//RegisterProxyGW impl server.APIGatewayProxy
func (srv *announceService) RegisterProxyGW(ctx context.Context, mux *grpcRt.ServeMux, c *grpc.ClientConn) error {
	return announcer.RegisterAnnounceServiceHandler(ctx, mux, c)
}

//OnStop impl server.APIServiceOnStopEvent
func (srv *announceService) OnStop() {
	srv.stop()
}

//GetState ...
func (srv *announceService) GetState(ctx context.Context, _ *emptypb.Empty) (resp *announcer.GetStateResponse, err error) {
	var leave func()
	if leave, err = srv.enter(ctx); err != nil {
		return
	}
	defer func() {
		leave()
		err = srv.correctError(err)
	}()

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("net-interface", srv.netInterface.Name))
	var addrs []net.Addr
	if addrs, err = srv.netInterface.Addrs(); err != nil {
		err = errors.WithMessagef(err, "get addresses from '%s' interface",
			srv.netInterface.Name)
		return
	}
	resp = new(announcer.GetStateResponse)
	resp.Services = make([]string, 0, len(addrs))
	for _, a := range addrs {
		resp.Services = append(resp.Services, a.String())
	}
	sort.Strings(resp.Services)
	_ = slice.DedupSlice(&resp.Services, func(i, j int) bool {
		return resp.Services[i] == resp.Services[j]
	})
	return resp, nil
}

//RemoveIP ...
func (srv *announceService) RemoveIP(ctx context.Context, req *announcer.RemoveIpRequest) (resp *emptypb.Empty, err error) {
	var leave func()
	if leave, err = srv.enter(ctx); err != nil {
		return
	}
	defer func() {
		leave()
		err = srv.correctError(err)
	}()

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("net-interface", srv.netInterface.Name),
		attribute.String("IP-to-remove", req.GetIp()),
	)

	ip := req.GetIp()
	if len(ip) == 0 || net.ParseIP(ip) == nil {
		err = status.Errorf(codes.InvalidArgument, "provided invalid IP(%s)", ip)
		return
	}

	addr2del := ip + mask32
	var found bool
	if found, err = srv.isAddrIn(addr2del); err != nil {
		return
	}
	if !found {
		err = status.Errorf(codes.NotFound, "addr '%s' is not found from '%s' interface",
			addr2del, srv.netInterface.Name)
		return
	}
	var lnk netlink.Link
	if lnk, err = netlink.LinkByName(srv.netInterface.Name); err != nil {
		err = errors.WithMessagef(err, "netlink/LinkByName('%s')", srv.netInterface.Name)
		return
	}
	var addr *netlink.Addr
	if addr, err = netlink.ParseAddr(addr2del); err != nil {
		err = errors.WithMessagef(err, "netlink/LinkByName('%s')", addr2del)
		return
	}
	if err = netlink.AddrDel(lnk, addr); err != nil {
		err = errors.WithMessagef(err, "netlink/AddrDel('%s')", addr)
		return
	}
	return new(emptypb.Empty), nil
}

//AddIP ...
func (srv *announceService) AddIP(ctx context.Context, req *announcer.AddIpRequest) (resp *emptypb.Empty, err error) {
	var leave func()
	if leave, err = srv.enter(ctx); err != nil {
		return
	}
	defer func() {
		leave()
		err = srv.correctError(err)
	}()

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("net-interface", srv.netInterface.Name),
		attribute.String("IP-to-add", req.GetIp()),
	)

	ip := req.GetIp()
	if len(ip) == 0 || net.ParseIP(ip) == nil {
		err = status.Errorf(codes.InvalidArgument, "provided invalid IP(%s)", ip)
		return
	}
	addr2Add := ip + mask32
	var found bool
	if found, err = srv.isAddrIn(addr2Add); err != nil {
		return
	}
	if found {
		err = status.Errorf(codes.AlreadyExists, "addr '%s' already exist in '%s' interface",
			addr2Add, srv.netInterface.Name)
		return
	}

	var lnk netlink.Link
	if lnk, err = netlink.LinkByName(srv.netInterface.Name); err != nil {
		err = errors.WithMessagef(err, "netlink/LinkByName('%s')", srv.netInterface.Name)
		return
	}
	var addr *netlink.Addr
	if addr, err = netlink.ParseAddr(addr2Add); err != nil {
		err = errors.WithMessagef(err, "netlink/ParseAddr('%s')", addr2Add)
		return
	}
	if err = netlink.AddrAdd(lnk, addr); err != nil {
		err = errors.WithMessagef(err, "netlink/AddrAdd('%s')", addr)
		return
	}
	return new(emptypb.Empty), nil
}

func (srv *announceService) correctError(err error) error {
	if err != nil && status.Code(err) == codes.Unknown {
		switch errors.Cause(err) {
		case context.DeadlineExceeded:
			return status.New(codes.DeadlineExceeded, err.Error()).Err()
		case context.Canceled:
			return status.New(codes.Canceled, err.Error()).Err()
		default:
			if e := new(url.Error); errors.As(err, &e) {
				switch errors.Cause(e.Err) {
				case context.Canceled:
					return status.New(codes.Canceled, err.Error()).Err()
				case context.DeadlineExceeded:
					return status.New(codes.DeadlineExceeded, err.Error()).Err()
				default:
					if e.Timeout() {
						return status.New(codes.DeadlineExceeded, err.Error()).Err()
					}
				}
			}
			err = status.New(codes.Internal, err.Error()).Err()
		}
	}
	return err
}

func (srv *announceService) enter(ctx context.Context) (leave func(), err error) {
	select {
	case <-srv.appCtx.Done():
		err = srv.appCtx.Err()
	case <-ctx.Done():
		err = ctx.Err()
	case srv.sema <- struct{}{}:
		var o sync.Once
		leave = func() {
			o.Do(func() {
				<-srv.sema
			})
		}
		return
	}
	err = status.FromContextError(err).Err()
	return
}

func (srv *announceService) isAddrIn(addr string) (bool, error) {
	addrs, err := srv.netInterface.Addrs()
	if err != nil {
		return false, errors.WithMessagef(err, "check is addr '%s' in", addr)
	}
	for _, a := range addrs {
		if a.String() == addr {
			return true, nil
		}
	}
	return false, nil
}
