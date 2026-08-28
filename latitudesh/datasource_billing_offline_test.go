package latitudesh

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/latitudesh/latitudesh-go-sdk/models/components"
)

var basicObjectAsOptions = basetypes.ObjectAsOptions{}

func TestTimeValue(t *testing.T) {
	if got := timeValue(nil); !got.IsNull() {
		t.Errorf("timeValue(nil) = %v, want null", got)
	}

	ts := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	got := timeValue(&ts)
	if got.IsNull() {
		t.Fatal("timeValue(non-nil) is null, want a known value")
	}
	if want := "2026-08-01T00:00:00Z"; got.ValueString() != want {
		t.Errorf("timeValue() = %q, want %q", got.ValueString(), want)
	}
}

func TestBillingPeriodValueNil(t *testing.T) {
	ctx := context.Background()
	obj, diags := billingPeriodValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("billingPeriodValue(nil) diagnostics: %v", diags)
	}
	if !obj.IsNull() {
		t.Error("billingPeriodValue(nil) is not null")
	}
}

// billingUsageProductsPayload is a representative /billing/usage products
// array covering a metered server line (with metadata.servers) and a
// discounted, non-metered line, so the mapping is exercised for both shapes.
const billingUsageProductsPayload = `[
  {
    "id": "prod_1",
    "resource": "servers",
    "name": "Server usage",
    "proration": false,
    "discountable": true,
    "description": "c2-small-x86",
    "amount_without_discount": 5000,
    "start": "2026-08-01T00:00:00Z",
    "end": "2026-08-31T23:59:59Z",
    "unit": "quantity",
    "unit_amount": 100,
    "unit_price": 1.5,
    "usage_type": "metered",
    "quantity": 50,
    "amount": 5000,
    "price": 75,
    "discounts": [
      {"description": "loyalty discount", "type": "percent", "value": 10}
    ],
    "metadata": {
      "servers": [
        {"id": "srv_1", "hostname": "web-01", "plan": "c2-small-x86", "tags": ["prod", "web"]}
      ]
    }
  },
  {
    "id": "prod_2",
    "resource": "object_storage",
    "name": "Storage usage",
    "proration": true,
    "discountable": false,
    "amount": 200,
    "price": 3,
    "metadata": {
      "bucket": {"id": "bucket_1", "name": "backups", "location": "SAO2"},
      "billing_unit_divisor": 1000
    }
  }
]`

func TestBillingProductsValueMapping(t *testing.T) {
	ctx := context.Background()

	var products []components.Products
	if err := json.Unmarshal([]byte(billingUsageProductsPayload), &products); err != nil {
		t.Fatalf("unmarshaling products payload: %s", err)
	}

	list, diags := billingProductsValue(ctx, products)
	if diags.HasError() {
		t.Fatalf("billingProductsValue diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("products list is null; want a known list")
	}

	var got []BillingProductModel
	if d := list.ElementsAs(ctx, &got, false); d.HasError() {
		t.Fatalf("ElementsAs: %v", d)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 products, got %d", len(got))
	}

	first := got[0]
	if first.ID.ValueString() != "prod_1" {
		t.Errorf("product 0 id = %q, want prod_1", first.ID.ValueString())
	}
	if first.Start.ValueString() != "2026-08-01T00:00:00Z" {
		t.Errorf("product 0 start = %q, want RFC3339 start", first.Start.ValueString())
	}

	var discounts []BillingDiscountModel
	if d := first.Discounts.ElementsAs(ctx, &discounts, false); d.HasError() {
		t.Fatalf("ElementsAs discounts: %v", d)
	}
	if len(discounts) != 1 {
		t.Fatalf("expected 1 discount, got %d", len(discounts))
	}
	if discounts[0].Type.ValueString() != "percent" {
		t.Errorf("discount type = %q, want percent", discounts[0].Type.ValueString())
	}
	if discounts[0].Value.ValueFloat64() != 10 {
		t.Errorf("discount value = %v, want 10", discounts[0].Value.ValueFloat64())
	}

	if first.Metadata.IsNull() {
		t.Fatal("product 0 metadata is null; want servers metadata")
	}
	var meta1 BillingMetadataModel
	if d := first.Metadata.As(ctx, &meta1, basicObjectAsOptions); d.HasError() {
		t.Fatalf("metadata As: %v", d)
	}
	var servers []BillingServerModel
	if d := meta1.Servers.ElementsAs(ctx, &servers, false); d.HasError() {
		t.Fatalf("ElementsAs servers: %v", d)
	}
	if len(servers) != 1 || servers[0].Hostname.ValueString() != "web-01" {
		t.Fatalf("unexpected servers mapping: %+v", servers)
	}
	if meta1.Bucket.IsNull() == false {
		t.Error("product 0 metadata.bucket should be null (no bucket in payload)")
	}

	second := got[1]
	if second.Discounts.IsNull() {
		t.Error("product 1 discounts should be a known empty list, not null")
	}
	if n := len(second.Discounts.Elements()); n != 0 {
		t.Errorf("product 1 discounts length = %d, want 0", n)
	}
	if second.Metadata.IsNull() {
		t.Fatal("product 1 metadata is null; want bucket metadata")
	}
	var meta2 BillingMetadataModel
	if d := second.Metadata.As(ctx, &meta2, basicObjectAsOptions); d.HasError() {
		t.Fatalf("metadata As: %v", d)
	}
	if meta2.Bucket.IsNull() {
		t.Fatal("product 1 metadata.bucket is null; want a known bucket")
	}
	var bucket BillingBucketModel
	if d := meta2.Bucket.As(ctx, &bucket, basicObjectAsOptions); d.HasError() {
		t.Fatalf("bucket As: %v", d)
	}
	if bucket.Name.ValueString() != "backups" {
		t.Errorf("bucket name = %q, want backups", bucket.Name.ValueString())
	}
	if meta2.BillingUnitDivisor.ValueInt64() != 1000 {
		t.Errorf("billing_unit_divisor = %v, want 1000", meta2.BillingUnitDivisor.ValueInt64())
	}
}

// TestBillingProductsValueEmptyNeverNull guards the "always a list, never
// null" guarantee (mirrors planDrivesValue) so a config that iterates
// `products` never fails on a null iteratee for a report with no usage yet.
func TestBillingProductsValueEmptyNeverNull(t *testing.T) {
	ctx := context.Background()
	list, diags := billingProductsValue(ctx, nil)
	if diags.HasError() {
		t.Fatalf("billingProductsValue(nil) diagnostics: %v", diags)
	}
	if list.IsNull() {
		t.Fatal("billingProductsValue(nil) returned a null list; want an empty known list")
	}
	if n := len(list.Elements()); n != 0 {
		t.Fatalf("billingProductsValue(nil) length = %d, want 0", n)
	}
}
