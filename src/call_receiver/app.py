import json
import boto3
import requests
import os
import re

secretsmanager = boto3.client('secretsmanager')
TOKEN_SECRET_NAME = os.environ['FLUX2_WEBHOOK_TOKEN_SECRET_NAME']
WEBHOOK_MAP_SECRET_NAME = os.environ['REPOS_MAPPING']
default_token = None
webhook_map = None
session = requests.Session()


def get_global_token():
    global default_token
    if not default_token:
        response = secretsmanager.get_secret_value(SecretId=TOKEN_SECRET_NAME)
        default_token = response['SecretString']
    return default_token


def get_webhook_map():
    global webhook_map
    if not webhook_map:
        response = secretsmanager.get_secret_value(SecretId=WEBHOOK_MAP_SECRET_NAME)
        webhook_map = json.loads(response['SecretString'])
    return webhook_map


def process_ecr_push_event(detail):
    repository = detail['repository-name']
    image_digest = detail['image-digest']
    image_tag = detail['image-tag']

    print(json.dumps({
        'message': 'A new image has been pushed to the repository',
        'repository': repository,
        'image_digest': image_digest,
        'image_tag': image_tag
    }))


def make_requests(webhook_url, repository, headers):
    # Call the Flux webhook with the token and corresponding endpoint
    if webhook_url:
        response = session.post(webhook_url, headers=headers)
        print(json.dumps({
            'message': 'Webhook response',
            'status_code': response.status_code
        }))
    else:
        print(json.dumps({
            'message': 'No webhook endpoint found for repository',
            'repository': repository
        }))


def call_flux_webhook(repository, image_tag):
    # Retrieve the map of values from Secrets Manager
    webhook_map = get_webhook_map()

    # Find the webhook endpoint corresponding to the event repository
    webhook_url = None
    token = None
    if repository in webhook_map:
        repo_data = webhook_map[repository]
        for key, data in repo_data.items():
            webhook_urls = data.get('webhook')
            token = data.get('token', get_global_token())
            regex = data.get('regex', '.*')
            for webhook in webhook_urls:
                headers = {'Authorization': f'Bearer {token}'}
                if regex and re.match(regex, image_tag):
                    make_requests(webhook, repository, headers)


def lambda_handler(event, context):
    # Extract event information
    record = event['Records'][0]
    message = json.loads(record['body'])
    detail = message['detail']

    # Process the ECR push event
    process_ecr_push_event(detail)

    # Call the Flux webhook with the event repository
    call_flux_webhook(detail['repository-name'], detail['image-tag'])

    return {
        'statusCode': 200,
        'body': json.dumps('Event processed successfully')
    }
