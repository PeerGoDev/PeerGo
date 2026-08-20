package promotions

import "github.com/peergo/peergo/services/core/internal/platform/settlementcontrol"

// Compatibility aliases keep the promotion module's composition surface
// stable while promotion and H&R reuse one delivery implementation.
type PendingCommand = settlementcontrol.PendingCommand
type DeliveryRepository = settlementcontrol.Repository
type DeliverySink = settlementcontrol.Sink
type DispatcherConfig = settlementcontrol.DispatcherConfig
type Dispatcher = settlementcontrol.Dispatcher

var NewDispatcher = settlementcontrol.NewDispatcher
