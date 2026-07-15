local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- 1. Read the current state
local data = redis.call('HMGET', key, 'tokens', 'last_updated')
local tokens = tonumber(data[1])
local last_updated = tonumber(data[2])

-- 2. Calculate current token balance (Lazy Refill)
if not tokens then
    tokens = capacity
    last_updated = now
else
    local elapsed = math.max(0, now - last_updated)
    local generated = elapsed * rate
    tokens = math.min(capacity, tokens + generated)
    last_updated = now
end

-- 3. Determine the outcome variable
local allowed = 0
if tokens >= requested then
    tokens = tokens - requested
    allowed = 1
end

-- 4. Single Shared Write-Back Layer (Deduplication Point)
redis.call('HSET', key, 'tokens', tokens, 'last_updated', last_updated)
redis.call('EXPIRE', key, math.ceil(capacity / rate) * 2)

-- 5. Return the final status to Go
return allowed