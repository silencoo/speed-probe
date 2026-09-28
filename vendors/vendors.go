package vendors

import (
	"github.com/silencoo/speed-probe/interfaces"

	"github.com/silencoo/speed-probe/vendors/clash"
	"github.com/silencoo/speed-probe/vendors/invalid"
	"github.com/silencoo/speed-probe/vendors/local"
)

var registeredList = map[interfaces.VendorType]func() interfaces.Vendor{
	interfaces.VendorLocal: func() interfaces.Vendor {
		return &local.Local{}
	},
	interfaces.VendorClash: func() interfaces.Vendor {
		return &clash.Clash{}
	},
}

func Find(vendorType interfaces.VendorType) interfaces.Vendor {
	if vendor, ok := registeredList[vendorType]; ok {
		return vendor()
	}

	return &invalid.Invalid{}
}
