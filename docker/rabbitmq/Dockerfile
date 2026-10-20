FROM rabbitmq:3.13-management

# Install curl and certificates to download the delayed message exchange plugin
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && curl -fSL -o /opt/rabbitmq/plugins/rabbitmq_delayed_message_exchange-3.13.0.ez \
       https://github.com/rabbitmq/rabbitmq-delayed-message-exchange/releases/download/v3.13.0/rabbitmq_delayed_message_exchange-3.13.0.ez \
    && chown rabbitmq:rabbitmq /opt/rabbitmq/plugins/rabbitmq_delayed_message_exchange-3.13.0.ez \
    && rabbitmq-plugins enable --offline rabbitmq_delayed_message_exchange \
    && apt-get purge -y --auto-remove curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*
