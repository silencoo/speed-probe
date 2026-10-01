package vendors

import (
	"context"
	"github.com/silencoo/speed-probe/interfaces"

	"github.com/silencoo/speed-probe/vendors/clash"
	"github.com/silencoo/speed-probe/vendors/invalid"
	"github.com/silencoo/speed-probe/vendors/local"
	"github.com/silencoo/speed-probe/vendors/singbox"
)

var registeredList = map[interfaces.VendorType]func() interfaces.Vendor{
	interfaces.VendorMihomo:  func() interfaces.Vendor { return &clash.Clash{} },
	interfaces.VendorSingBox: func() interfaces.Vendor { return &singbox.SingBox{} },
	interfaces.VendorLocal: func() interfaces.Vendor {
		return &local.Local{}
	},
	interfaces.VendorClash: func() interfaces.Vendor {
		return &clash.Clash{}
	},
}

func Supported(kind interfaces.VendorType) bool { _, ok := registeredList[kind]; return ok }

func Build(ctx context.Context, kind interfaces.VendorType, name, payload string) interfaces.Vendor {
	v := Find(kind)
	if builder, ok := v.(interface {
		BuildContext(context.Context, string, string) interfaces.Vendor
	}); ok {
		return builder.BuildContext(ctx, name, payload)
	}
	return v.Build(name, payload)
}

func Find(vendorType interfaces.VendorType) interfaces.Vendor {
	if vendor, ok := registeredList[vendorType]; ok {
		return vendor()
	}

	return &invalid.Invalid{}
}

func BuildNode(ctx context.Context, kind interfaces.VendorType, node interfaces.SlaveRequestNode) interfaces.Vendor {
	if node.Path == nil {
		return Build(ctx, kind, node.Name, node.Payload)
	}
	if kind != interfaces.VendorSingBox {
		return &invalid.Invalid{}
	}
	return (&singbox.SingBox{}).BuildPathContext(ctx, node.Path)
}
