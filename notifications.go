package main

import (
	"fmt"
	"log"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

func SendNotification(target_username string, target_email string,
	subject string, post_url string, content string) {

	//@note: use sendgrid
	//Sendgrid: Add API key, then add a single sender gmail
	//@note: sendgrid doesnt seem to like it if you send to your own email ie. "dev.ddengster@gmail.com"
	from := mail.NewEmail("{SENDGRID_FROM}", "{SENDGRID_FROM_EMAIL}")
	to := mail.NewEmail(target_username, target_email)
	plainTextContent := "Hey " + target_username + ", a new reply has been posted!\n" + content
	htmlContent := "<strong>Hey " + target_username + "," +
		" a new <a clicktracking=\"off\" href=\"" + post_url + "\">reply</a> has been posted to your comment!</strong>\n" + content
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	client := sendgrid.NewSendClient("{SENDGRID_KEY}")
	response, err := client.Send(message)
	if err != nil {
		log.Println(err)
	} else {
		fmt.Println(response.StatusCode)
		fmt.Println(response.Body)
		fmt.Println(response.Headers)
	}
}
