# Checkout flow — design notes

The checkout flow has four components and one rule: nothing is charged
before the cart is priced.

## Cart

`Cart` holds the shopper's chosen items. It is the only component that
writes; everything downstream reads from it. A cart is immutable once the
customer reaches review — changes after that point start a new cart.

## Pricing

`Pricing` reads a cart and answers what it costs: line totals, the tax rate
per line (full rate 8.25%, discounted lines 5%), and the grand total. It
never talks to the payment side and holds no state of its own.

## PaymentGateway

`PaymentGateway` takes a priced cart — never a bare one — and asks the
processor to charge it. It retries a declined charge once, then hands the
failure back to the caller. It writes the authorization id onto the receipt.

## Receipt

`Receipt` records what was charged: the cart it came from, the pricing
breakdown it was given, the authorization id, and the timestamps. The
receipt is immutable once written; a refund is a new receipt, not an edit.

## How they connect

Cart → Pricing → PaymentGateway → Receipt, in one direction. If pricing
fails, the gateway is never called; if payment fails, no receipt is written
and the cart stays open for the customer to retry.
