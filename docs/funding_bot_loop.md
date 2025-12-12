# 8. Bot Loop (Pseudocode)

```
loop every 1s:
    if bot.enabled == false:
        continue

    funding = get_funding(exchange, symbol)
    if funding.rate <= 0:
        continue

    countdown = funding.next_time - now
    if countdown > threshold:
        continue

    orderbook = get_orderbook(exchange, symbol)

    if big_ask_wall_present and limit_price >= wall_price:
        continue

    limit_price = calc_limit_price(current_price, funding.rate)

    place_limit_short(exchange, symbol, limit_price)

    wait until funding event

    if order not filled:
        cancel_order()

done
```

---