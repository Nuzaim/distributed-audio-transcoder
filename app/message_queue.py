import os
from abc import ABC, abstractmethod
from boto3 import client as aws_client


class MessageQueueClient(ABC):
    @abstractmethod
    def send_message(self, queue_url: str, message_body: str, message_attributes: dict):
        """Send a message to the specified SQS queue."""
        pass

class SQSClient(MessageQueueClient):
    def __init__(self, region_name, endpoint_url, aws_access_key_id, aws_secret_access_key):
        self.client = aws_client(
            'sqs',
            region_name=region_name,
            endpoint_url=endpoint_url,
            aws_access_key_id=aws_access_key_id,
            aws_secret_access_key=aws_secret_access_key,
        )
    def send_message(self, queue_url: str, message_body: str, message_attributes: dict = {}):
        # change to queue by name.
        self.client.send_message(QueueUrl=queue_url, MessageBody=message_body, MessageAttributes=message_attributes)

def get_sqs_client():
    # TODO: change to config.
    endpoint_url_host = os.environ.get("SQS_ENDPOINT_URL", "http://localhost:9324")
    client = SQSClient(
        region_name='us-east-1',
        endpoint_url=endpoint_url_host,
        aws_access_key_id='mock_key',
        aws_secret_access_key='mock_secret',
    )
    yield client

